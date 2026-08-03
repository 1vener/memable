// netdrive.go：网盘（115）相关 HTTP 处理器。
// 设置页杂项参数（settings 表）读写、115 Cookie 验证、网盘目录树懒加载、
// 目录对齐补齐 SHA1 任务提交。
// 代码注释使用中文。
package api

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"memable/internal/pan115"
	"memable/internal/repo"
)

// handleListSettingsKV 返回全部杂项参数（设置页展示用；本地单用户应用）。
func (s *Server) handleListSettingsKV(w http.ResponseWriter, r *http.Request) {
	entries, err := s.settings.List()
	if err != nil {
		writeError(w, 500, "读取杂项参数失败: "+err.Error())
		return
	}
	writeJSON(w, 200, entries)
}

// handleSetSettingsKV 写入杂项参数（如 115 Cookie）。
func (s *Server) handleSetSettingsKV(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := parseJSON(r, &req); err != nil {
		writeError(w, 400, "请求体格式错误")
		return
	}
	if strings.TrimSpace(req.Key) == "" {
		writeError(w, 400, "key 不能为空")
		return
	}
	if err := s.settings.Set(req.Key, req.Value); err != nil {
		writeError(w, 500, "写入参数失败: "+err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"status": "ok", "key": req.Key})
}

// handleDeleteSettingsKV 删除杂项参数。
func (s *Server) handleDeleteSettingsKV(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key string `json:"key"`
	}
	if err := parseJSON(r, &req); err != nil {
		writeError(w, 400, "请求体格式错误")
		return
	}
	if err := s.settings.Delete(req.Key); err != nil {
		writeError(w, 500, "删除参数失败: "+err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"status": "ok"})
}

// handleVerifyNetdrive115 验证 115 Cookie 有效性（设置页填写后校验）。
func (s *Server) handleVerifyNetdrive115(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Cookie string `json:"cookie"`
	}
	if err := parseJSON(r, &req); err != nil {
		writeError(w, 400, "请求体格式错误")
		return
	}
	cookie := strings.TrimSpace(req.Cookie)
	if cookie == "" {
		writeError(w, 400, "Cookie 不能为空")
		return
	}
	client := pan115.NewClient(cookie, s.netdriveIntervalMs())
	if err := client.LoginCheck(r.Context()); err != nil {
		slog.Warn("115 Cookie 验证失败", "err", err)
		writeJSON(w, 200, map[string]any{"valid": false, "error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"valid": true})
}

// handleNetdrive115Tree 网盘目录树懒加载：返回指定 cid 的直属子目录。
func (s *Server) handleNetdrive115Tree(w http.ResponseWriter, r *http.Request) {
	cookie, err := s.settings.Get(repo.SettingsKeyNetdriveCookie)
	if err != nil {
		writeError(w, 500, "读取参数失败: "+err.Error())
		return
	}
	if strings.TrimSpace(cookie) == "" {
		writeError(w, 400, "未配置 115 Cookie，请在设置页填写")
		return
	}
	cid := r.URL.Query().Get("cid")
	if cid == "" {
		cid = "0"
	}
	client := pan115.NewClient(strings.TrimSpace(cookie), s.netdriveIntervalMs())
	dirs, err := client.ListDir(r.Context(), cid)
	if err != nil {
		writeError(w, 500, "读取 115 目录失败: "+err.Error())
		return
	}
	writeJSON(w, 200, dirs)
}

// handleNetdrive115SyncSha1 提交 115 补齐 SHA1 任务（网盘独立队列）。
func (s *Server) handleNetdrive115SyncSha1(w http.ResponseWriter, r *http.Request) {
	var req struct {
		LibraryID int64  `json:"library_id"`
		LocalDir  string `json:"local_dir"`
		RemoteCID string `json:"remote_cid"`
	}
	if err := parseJSON(r, &req); err != nil {
		writeError(w, 400, "请求体格式错误")
		return
	}
	if req.LibraryID <= 0 {
		writeError(w, 400, "library_id 必须为正整数")
		return
	}
	lib, err := s.libraries.GetByID(req.LibraryID)
	if err != nil || lib == nil {
		writeError(w, 404, "收藏库不存在")
		return
	}
	req.RemoteCID = strings.TrimSpace(req.RemoteCID)
	if req.RemoteCID == "" {
		writeError(w, 400, "remote_cid 不能为空")
		return
	}
	cookie, err := s.settings.Get(repo.SettingsKeyNetdriveCookie)
	if err != nil {
		writeError(w, 500, "读取参数失败: "+err.Error())
		return
	}
	if strings.TrimSpace(cookie) == "" {
		writeError(w, 400, "未配置 115 Cookie，请在设置页填写")
		return
	}
	if s.runner == nil {
		writeError(w, 500, "任务调度器未初始化")
		return
	}

	req.LocalDir = normalizePath(req.LocalDir)
	matchSize := s.cfg.Netdrive.MatchSize == nil || *s.cfg.Netdrive.MatchSize
	dedupeKey := "netdrive_sha1:" + strconv.FormatInt(req.LibraryID, 10) + ":" + req.RemoteCID
	task, err := s.runner.Enqueue(repo.TaskKindNetdriveSha1,
		"115 补齐 SHA1: "+req.LocalDir, &dedupeKey, &req.LibraryID,
		repo.NetdriveSyncPayload{
			LibraryID: req.LibraryID,
			LocalDir:  req.LocalDir,
			RemoteCID: req.RemoteCID,
			MatchSize: matchSize,
		})
	if err != nil {
		writeError(w, 409, "相同任务已在等待或执行")
		return
	}
	pos, _ := s.tasks.QueuePosition(task.ID)
	writeJSON(w, 202, map[string]any{
		"task_id":        task.ID,
		"status":         task.Status,
		"queue_position": pos,
	})
}

// netdriveIntervalMs 返回 115 请求间隔（配置优先，默认 300）。
func (s *Server) netdriveIntervalMs() int {
	if s.cfg != nil && s.cfg.Netdrive.RequestIntervalMs > 0 {
		return s.cfg.Netdrive.RequestIntervalMs
	}
	return 300
}
