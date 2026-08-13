// transcode.go：解码器不支持的媒体（如 ProRes）按需转码为 H.264 MP4 后播放。
// 转码为后台 goroutine 异步执行，状态经内存 map 保存（进程内幂等）；
// 产物文件内容寻址（media id + mtime），已存在且 mtime 一致时直接命中缓存。
// 代码注释使用中文。
package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"memable/internal/cmdx"
)

// transcodeStatus 转码任务状态。
const (
	transcodeRunning = "running"
	transcodeDone    = "done"
	transcodeFailed  = "failed"
)

// transcodeJob 单个媒体的转码任务状态。
type transcodeJob struct {
	status string
	path   string // 产物本地绝对路径（done 后有效）
	name   string // 产物文件名（web 端经 /api/transcode/{name} 播放）
	err    string
}

var (
	transcodeJobs sync.Map // mediaID(int64) -> *transcodeJob
	transcodeMu   sync.Mutex
)

// transcodeDir 转码产物临时目录（系统临时目录，重启即清）。
func transcodeDir() string {
	return filepath.Join(os.TempDir(), "memable-transcode")
}

// transcodeOutPath 计算转码产物路径：内容寻址 = media id + mtime。
func transcodeOutPath(id int64, mtime time.Time) string {
	return filepath.Join(transcodeDir(), fmt.Sprintf("%d-%d.mp4", id, mtime.Unix()))
}

// handleTranscode 启动/查询转码：已缓存（文件存在）或任务运行中直接返回状态；
// 否则启动后台 goroutine 转码并返回 running。
func (s *Server) handleTranscode(w http.ResponseWriter, r *http.Request) {
	id, err := parseInt64(r.PathValue("id"))
	if err != nil {
		writeError(w, 400, "无效的媒体 ID")
		return
	}
	m, src, err := s.resolveMediaAbsPath(id)
	if err != nil {
		var he *httpErr
		if errors.As(err, &he) {
			writeError(w, he.code, he.msg)
			return
		}
		writeError(w, 500, err.Error())
		return
	}
	if m.Kind != "video" {
		writeError(w, 400, "仅视频支持转码")
		return
	}

	out := transcodeOutPath(id, m.Mtime)
	// 1) 命中缓存：产物已存在且未过期（按 mtime 内容寻址，存在即有效）
	if _, err := os.Stat(out); err == nil {
		writeJSON(w, 200, map[string]any{
			"status": transcodeDone,
			"path":   out,
			"name":   filepath.Base(out),
		})
		return
	}
	// 2) 已有任务（运行中/失败）：返回其状态
	if v, ok := transcodeJobs.Load(id); ok {
		job := v.(*transcodeJob)
		writeJSON(w, 200, transcodeJobResp(job))
		return
	}
	// 3) 启动新任务（互斥防止同一媒体并发重复转码）
	transcodeMu.Lock()
	defer transcodeMu.Unlock()
	if v, ok := transcodeJobs.Load(id); ok {
		job := v.(*transcodeJob)
		writeJSON(w, 200, transcodeJobResp(job))
		return
	}
	job := &transcodeJob{status: transcodeRunning}
	transcodeJobs.Store(id, job)

	go func() {
		if err := os.MkdirAll(transcodeDir(), 0o755); err != nil {
			job.status = transcodeFailed
			job.err = "创建转码目录失败: " + err.Error()
			return
		}
		// 临时文件转正：转码中断不留下半成品缓存
		tmp := out + ".tmp"
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
		defer cancel()
		cmd := cmdx.Command(ctx, "ffmpeg",
			"-v", "error", "-y", "-i", src,
			"-c:v", "libx264", "-preset", "veryfast", "-crf", "23",
			"-pix_fmt", "yuv420p",
			"-c:a", "aac", "-b:a", "128k",
			"-movflags", "+faststart",
			tmp,
		)
		if outLog, err := cmd.CombinedOutput(); err != nil {
			_ = os.Remove(tmp)
			job.status = transcodeFailed
			job.err = fmt.Sprintf("转码失败: %v\n%s", err, truncateLog(string(outLog)))
			slog.Error("媒体转码失败", "media_id", id, "err", err)
			return
		}
		if err := os.Rename(tmp, out); err != nil {
			_ = os.Remove(tmp)
			job.status = transcodeFailed
			job.err = "转码产物落盘失败: " + err.Error()
			return
		}
		job.status = transcodeDone
		job.path = out
		job.name = filepath.Base(out)
		slog.Info("媒体转码完成", "media_id", id, "path", out)
	}()

	writeJSON(w, 200, transcodeJobResp(job))
}

// handleTranscodeStatus 查询转码任务状态（前端轮询）。
func (s *Server) handleTranscodeStatus(w http.ResponseWriter, r *http.Request) {
	id, err := parseInt64(r.PathValue("id"))
	if err != nil {
		writeError(w, 400, "无效的媒体 ID")
		return
	}
	m, err := s.media.GetByID(id)
	if err != nil {
		writeError(w, 500, "查询媒体失败: "+err.Error())
		return
	}
	if m == nil {
		writeError(w, 404, "媒体不存在")
		return
	}
	// 任务结束但内存状态丢失（服务重启）时，按产物文件兜底判定
	if v, ok := transcodeJobs.Load(id); ok {
		writeJSON(w, 200, transcodeJobResp(v.(*transcodeJob)))
		return
	}
	out := transcodeOutPath(id, m.Mtime)
	if _, err := os.Stat(out); err == nil {
		writeJSON(w, 200, map[string]any{
			"status": transcodeDone,
			"path":   out,
			"name":   filepath.Base(out),
		})
		return
	}
	writeJSON(w, 200, map[string]any{"status": transcodeFailed, "error": "转码任务不存在或已失效"})
}

// transcodeJobResp 转码任务对外响应。
func transcodeJobResp(job *transcodeJob) map[string]any {
	resp := map[string]any{"status": job.status}
	if job.path != "" {
		resp["path"] = job.path
	}
	if job.name != "" {
		resp["name"] = job.name
	}
	if job.err != "" {
		resp["error"] = job.err
	}
	return resp
}

// handleTranscodeFile 提供转码产物字节（web 端播放；桌面端直接播本地路径）。
// 文件名白名单校验：仅允许转码目录内的 .mp4 文件，防路径穿越。
func (s *Server) handleTranscodeFile(w http.ResponseWriter, r *http.Request) {
	name := filepath.Base(r.PathValue("name"))
	if name == "." || name == ".." || !strings.HasSuffix(name, ".mp4") {
		writeError(w, 400, "文件名非法")
		return
	}
	abs := filepath.Join(transcodeDir(), name)
	f, err := os.Open(abs)
	if err != nil {
		writeError(w, 404, "转码产物不存在")
		return
	}
	defer f.Close()
	w.Header().Set("Content-Disposition", "inline")
	http.ServeContent(w, r, name, time.Time{}, f)
}

// truncateLog 截断转码错误日志（最多 2000 字符）避免响应过大。
func truncateLog(s string) string {
	if len(s) > 2000 {
		return s[:2000]
	}
	return s
}
