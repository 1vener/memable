// 包 api：HTTP REST API 服务。
// 代码注释使用中文。
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"memable/internal/cmdx"
	"memable/internal/config"
	"memable/internal/duplicate"
	"memable/internal/repo"
	"memable/internal/scan"
	"memable/internal/search"
	"memable/internal/task"
)

// ffmpegCaps 缓存 FFmpeg/ffprobe 能力（服务启动时执行一次）。
type ffmpegCaps struct {
	Available  bool   `json:"available"`
	Version    string `json:"version"`
	HEICDecode bool   `json:"heic_decode"`
	CR2Decode  bool   `json:"cr2_decode"`
}

// ffprobeCaps 缓存 ffprobe 能力。
type ffprobeCaps struct {
	Available bool   `json:"available"`
	Version   string `json:"version"`
}

// Server HTTP API 服务器。
type Server struct {
	cfg            *config.Config
	libraries      *repo.LibraryRepo
	sessions       *repo.SessionRepo
	media          *repo.MediaRepo
	tasks          *repo.TaskRepo
	fileStats      *repo.FileStatsRepo
	settings       *repo.SettingsRepo // 杂项参数（115 Cookie 等）
	scanSvc        *scan.Service
	searchSvc      *search.Service
	runner         *task.Runner
	dup            *duplicate.Service
	imageThumbBase string
	videoThumbBase string
	ffmpegCaps     *ffmpegCaps
	ffprobeCaps    *ffprobeCaps
	http           *http.Server
	port           int // 实际监听端口（自动避让/随机后）
}

// NewServer 创建 HTTP API 服务器。
func NewServer(cfg *config.Config, lr *repo.LibraryRepo, sr *repo.SessionRepo, mr *repo.MediaRepo, tr *repo.TaskRepo, fsr *repo.FileStatsRepo, settings *repo.SettingsRepo, scanSvc *scan.Service, searchSvc *search.Service, runner *task.Runner, imageThumbBase, videoThumbBase string, dup *duplicate.Service) *Server {
	s := &Server{
		cfg:            cfg,
		libraries:      lr,
		sessions:       sr,
		media:          mr,
		tasks:          tr,
		fileStats:      fsr,
		settings:       settings,
		scanSvc:        scanSvc,
		searchSvc:      searchSvc,
		runner:         runner,
		dup:            dup,
		imageThumbBase: imageThumbBase,
		videoThumbBase: videoThumbBase,
	}
	s.probeFFmpegCaps()
	mux := http.NewServeMux()
	s.registerRoutes(mux)
	s.http = &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:      corsMiddleware(mux),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 120 * time.Second,
	}
	return s
}

// ActualPort 返回实际监听端口（Start 成功后有效；随机端口场景下前端以此为准）。
func (s *Server) ActualPort() int {
	return s.port
}

// registerRoutes 注册所有 API 路由。
func (s *Server) registerRoutes(mux *http.ServeMux) {
	// 阶段 8：收藏库管理
	mux.HandleFunc("GET /api/libraries", s.handleListLibraries)
	mux.HandleFunc("POST /api/libraries", s.handleCreateLibrary)
	mux.HandleFunc("GET /api/libraries/{id}", s.handleGetLibrary)
	mux.HandleFunc("PUT /api/libraries/{id}", s.handleUpdateLibrary)
	mux.HandleFunc("DELETE /api/libraries/{id}", s.handleDeleteLibrary)
	mux.HandleFunc("GET /api/libraries/{id}/tree", s.handleFileTree)
	mux.HandleFunc("GET /api/libraries/{id}/files", s.handleListFiles)
	mux.HandleFunc("DELETE /api/libraries/{id}/directories", s.handleDeleteDirectory)
	mux.HandleFunc("POST /api/libraries/{id}/directories/rename", s.handleRenameDirectory)
	mux.HandleFunc("POST /api/libraries/{id}/directories/move", s.handleMoveDirectory)

	// 扫描
	mux.HandleFunc("POST /api/libraries/{id}/scan", s.handleScanLibrary)
	mux.HandleFunc("POST /api/libraries/{id}/promote", s.handlePromoteLibrary)
	mux.HandleFunc("POST /api/libraries/{id}/scan-sha1", s.handleScanSha1)
	mux.HandleFunc("POST /api/scan/temporary", s.handleScanTemporary)
	mux.HandleFunc("GET /api/sessions/{id}", s.handleGetSession)
	mux.HandleFunc("POST /api/sessions/{id}/cancel", s.handleCancelSession)
	mux.HandleFunc("POST /api/sessions/{id}/promote", s.handlePromoteSession)

	// 搜索（阶段 7）
	mux.HandleFunc("GET /api/search", s.handleSearchText)
	mux.HandleFunc("POST /api/search/image", s.handleSearchImage)
	mux.HandleFunc("POST /api/search/image/upload", s.handleSearchImageUpload)

	// 重复报告（阶段 6）
	mux.HandleFunc("POST /api/reports/image", s.handleImageReport)
	mux.HandleFunc("POST /api/reports/video", s.handleVideoReport)
	mux.HandleFunc("POST /api/reports/duplicate", s.handleCreateDuplicateReport)
	mux.HandleFunc("GET /api/reports/duplicate", s.handleGetDuplicateReport)
	mux.HandleFunc("GET /api/reports/duplicate/groups", s.handleListDuplicateGroups)
	mux.HandleFunc("GET /api/reports/duplicate/tree", s.handleDuplicateReportTree)
	mux.HandleFunc("GET /api/reports/duplicate/defaults", s.handleGetDuplicateReportDefaults)
	mux.HandleFunc("POST /api/reports/duplicate/clear", s.handleClearDuplicateReport)
	mux.HandleFunc("POST /api/reports/duplicate/exclude", s.handleExcludeDuplicateMedia)
	mux.HandleFunc("POST /api/reports/directory-compare", s.handleCreateDirCompare)
	mux.HandleFunc("GET /api/reports/directory-compare", s.handleGetDirCompare)
	mux.HandleFunc("GET /api/reports/directory-compare/groups", s.handleListDirCompareGroups)
	mux.HandleFunc("GET /api/reports/directory-compare/tree", s.handleDirCompareTree)
	mux.HandleFunc("POST /api/reports/directory-compare/exclude", s.handleExcludeDirCompareMedia)
	mux.HandleFunc("POST /api/reports/directory-compare/clear", s.handleClearDirCompare)

	// 缩略图静态服务：/api/thumbnails/{kind}/{rel}，kind ∈ image/video
	mux.HandleFunc("GET /api/thumbnails/{kind}/", s.handleThumbnail)

	// 媒体操作
	mux.HandleFunc("POST /api/media/{id}/open", s.handleOpenMedia)
	mux.HandleFunc("POST /api/media/delete", s.handleDeleteMedia)

	// 健康检查
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/settings", s.handleSettings)
	mux.HandleFunc("GET /api/settings/kv", s.handleListSettingsKV)
	mux.HandleFunc("PUT /api/settings/kv", s.handleSetSettingsKV)
	mux.HandleFunc("DELETE /api/settings/kv", s.handleDeleteSettingsKV)

	// 网盘（115）
	mux.HandleFunc("POST /api/netdrive/115/verify", s.handleVerifyNetdrive115)
	mux.HandleFunc("GET /api/netdrive/115/tree", s.handleNetdrive115Tree)
	mux.HandleFunc("POST /api/netdrive/115/sync-sha1", s.handleNetdrive115SyncSha1)

	// 任务管理
	mux.HandleFunc("GET /api/tasks", s.handleListTasks)
	mux.HandleFunc("GET /api/tasks/{id}", s.handleGetTask)
	mux.HandleFunc("POST /api/tasks/{id}/cancel", s.handleCancelTask)

	// 工具 - 文件统计
	mux.HandleFunc("POST /api/tools/file-stats", s.handleCreateFileStats)
	mux.HandleFunc("GET /api/tools/file-stats", s.handleListFileStats)
	mux.HandleFunc("GET /api/tools/file-stats/{id}", s.handleGetFileStats)
	mux.HandleFunc("POST /api/tools/file-stats/{id}/diff", s.handleFileStatsDiff)
	mux.HandleFunc("POST /api/tools/file-stats/{id}/diff/export", s.handleFileStatsDiffExport)
	mux.HandleFunc("DELETE /api/tools/file-stats/{id}", s.handleDeleteFileStats)
}

// Start 启动 HTTP 服务器。指定端口被占用（EADDRINUSE）时自动 +1 避让，
// 最多尝试 20 个端口；配置端口为 0 时使用随机空闲端口。
// 实际监听端口通过 ActualPort() 获取。
func (s *Server) Start() error {
	base := s.cfg.Server.Port
	maxAttempts := 20
	if base == 0 {
		maxAttempts = 1
	}
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		port := base + attempt
		addr := fmt.Sprintf(":%d", port)
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			lastErr = err
			if isAddrInUse(err) {
				slog.Warn("端口被占用，尝试下一个端口", "addr", addr, "err", err)
				continue
			}
			return fmt.Errorf("监听 %s: %w", addr, err)
		}
		if port == 0 {
			// 随机端口：实际端口从监听器获取
			s.port = ln.Addr().(*net.TCPAddr).Port
		} else {
			s.port = port
		}
		slog.Info("HTTP API 启动", "addr", addr)
		return s.http.Serve(ln)
	}
	return fmt.Errorf("端口 %d~%d 均被占用: %w", base, base+maxAttempts-1, lastErr)
}

// isAddrInUse 判断端口占用错误。Windows 的错误码是 10048（WSAEADDRINUSE），
// 与 Go 的 syscall.EADDRINUSE（Unix 语义常量）不同，需一并判断；
// 文本匹配兜底覆盖各平台不同表述。
func isAddrInUse(err error) bool {
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "in use") ||
		strings.Contains(msg, "address already in use") ||
		strings.Contains(msg, "only one usage of each socket address") {
		return true
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return errno == syscall.EADDRINUSE || errno == 10048 // WSAEADDRINUSE
	}
	return false
}

// Shutdown 优雅关闭。
func (s *Server) Shutdown(ctx context.Context) error {
	return s.http.Shutdown(ctx)
}

// ===== 中间件 =====

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ===== 辅助函数 =====

// probeFFmpegCaps 探测 FFmpeg/ffprobe 安装和能力（启动时执行一次，缓存结果）。
func (s *Server) probeFFmpegCaps() {
	s.ffmpegCaps = &ffmpegCaps{}
	if v, err := exec.LookPath("ffmpeg"); err == nil {
		s.ffmpegCaps.Available = true
		if out, err := cmdx.CommandNoCtx(v, "-version").Output(); err == nil {
			// 取版本第一行
			if lines := strings.SplitN(string(out), "\n", 2); len(lines) > 0 {
				s.ffmpegCaps.Version = strings.TrimSpace(lines[0])
			}
		}
		// 探测 HEIC 解码能力
		if out, err := cmdx.CommandNoCtx(v, "-decoders").Output(); err == nil {
			s.ffmpegCaps.HEICDecode = strings.Contains(string(out), "hevc") || strings.Contains(string(out), "heic")
		}
		// 探测 CR2 解码能力
		if out, err := cmdx.CommandNoCtx(v, "-formats").Output(); err == nil {
			s.ffmpegCaps.CR2Decode = strings.Contains(string(out), "cr2") || strings.Contains(string(out), "raw")
		}
		slog.Info("ffmpeg 能力探测完成",
			"version", s.ffmpegCaps.Version,
			"heic", s.ffmpegCaps.HEICDecode,
			"cr2", s.ffmpegCaps.CR2Decode,
		)
	}

	s.ffprobeCaps = &ffprobeCaps{}
	if v, err := exec.LookPath("ffprobe"); err == nil {
		s.ffprobeCaps.Available = true
		if out, err := cmdx.CommandNoCtx(v, "-version").Output(); err == nil {
			if lines := strings.SplitN(string(out), "\n", 2); len(lines) > 0 {
				s.ffprobeCaps.Version = strings.TrimSpace(lines[0])
			}
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func parseJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

func parseInt64(s string) (int64, error) {
	var n int64
	_, err := fmt.Sscanf(s, "%d", &n)
	return n, err
}

// fullPath 拼接库路径和相对路径。
func fullPath(libPath, relPath string) string {
	return filepath.Join(libPath, relPath)
}

// thumbAbsPath 返回缩略图的绝对路径（按类型选择根目录）。
func (s *Server) thumbAbsPath(kind, relPath string) string {
	base := s.imageThumbBase
	if kind == "video" {
		base = s.videoThumbBase
	}
	if base == "" {
		base = "thumbnail"
	}
	return filepath.Join(base, relPath)
}

// ensureDir 确保目录存在。
func ensureDir(path string) error {
	return os.MkdirAll(path, 0o755)
}

// moveFile 移动文件（跨目录）。
func moveFile(src, dst string) error {
	if err := ensureDir(filepath.Dir(dst)); err != nil {
		return err
	}
	// 先尝试 rename（同卷快速），失败则复制+删除
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return err
	}
	return os.Remove(src)
}

// joinPath 统一路径拼接，返回正斜杠分隔的路径。
func joinPath(parts ...string) string {
	return strings.ReplaceAll(filepath.Join(parts...), "\\", "/")
}
