// netdrive.go：网盘（CloudDrive2）相关 HTTP 处理器。
// 设置页杂项参数（settings 表）读写、CD2 地址/Token 验证、CD2 目录树懒加载、
// 目录对齐补齐 SHA1 任务提交。所有 CD2 调用共享全局限速（见 internal/cd2）。
// 代码注释使用中文。
package api

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"memable/internal/cd2"
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

// handleSetSettingsKV 写入杂项参数（如 CD2 地址/Token）。
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

// handleVerifyNetdriveCD2 验证 CD2 地址与 API Token 有效性（设置页填写后校验）。
func (s *Server) handleVerifyNetdriveCD2(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Address string `json:"address"`
		Token   string `json:"token"`
	}
	if err := parseJSON(r, &req); err != nil {
		writeError(w, 400, "请求体格式错误")
		return
	}
	token := strings.TrimSpace(req.Token)
	if token == "" {
		writeJSON(w, 200, map[string]any{"valid": false, "error": "API Token 不能为空"})
		return
	}
	client := cd2.NewClient(req.Address, token)
	if err := client.Ping(r.Context()); err != nil {
		slog.Warn("CD2 服务不可达", "err", err)
		writeJSON(w, 200, map[string]any{"valid": false, "error": err.Error()})
		return
	}
	if err := client.VerifyToken(r.Context()); err != nil {
		slog.Warn("CD2 Token 校验失败", "err", err)
		writeJSON(w, 200, map[string]any{"valid": false, "error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"valid": true})
}

// netdriveAddr 读取已保存的 CD2 地址；未配置时回退配置默认值。
func (s *Server) netdriveAddr() string {
	addr, err := s.settings.Get(repo.SettingsKeyNetdriveAddr)
	if err == nil && strings.TrimSpace(addr) != "" {
		return strings.TrimSpace(addr)
	}
	if s.cfg != nil && s.cfg.Netdrive.CD2Address != "" {
		return s.cfg.Netdrive.CD2Address
	}
	return cd2.DefaultAddr
}

// netdriveClient 构造 CD2 客户端（地址来自设置/配置，Token 来自设置）。
func (s *Server) netdriveClient() (*cd2.Client, string, error) {
	token, err := s.settings.Get(repo.SettingsKeyNetdriveToken)
	if err != nil {
		return nil, "", err
	}
	if strings.TrimSpace(token) == "" {
		return nil, "", nil
	}
	return cd2.NewClient(s.netdriveAddr(), token), strings.TrimSpace(token), nil
}

// handleNetdriveCD2Tree CD2 目录树懒加载：返回指定路径的直属子目录。
func (s *Server) handleNetdriveCD2Tree(w http.ResponseWriter, r *http.Request) {
	client, _, err := s.netdriveClient()
	if err != nil {
		writeError(w, 500, "读取参数失败: "+err.Error())
		return
	}
	if client == nil {
		writeError(w, 400, "未配置 CloudDrive2 API Token，请在设置页填写")
		return
	}
	path := r.URL.Query().Get("path")
	if path == "" {
		path = "/"
	}
	dirs, err := client.ListDirs(r.Context(), path)
	if err != nil {
		writeError(w, 500, "读取 CloudDrive2 目录失败: "+err.Error())
		return
	}
	writeJSON(w, 200, dirs)
}

// handleNetdriveCD2SyncSha1 提交 CD2 补齐 SHA1 任务（网盘独立队列）。
func (s *Server) handleNetdriveCD2SyncSha1(w http.ResponseWriter, r *http.Request) {
	var req struct {
		LibraryID  int64  `json:"library_id"`
		LocalDir   string `json:"local_dir"`
		RemotePath string `json:"remote_path"`
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
	req.RemotePath = strings.TrimSpace(req.RemotePath)
	if req.RemotePath == "" {
		writeError(w, 400, "remote_path 不能为空")
		return
	}
	client, _, err := s.netdriveClient()
	if err != nil {
		writeError(w, 500, "读取参数失败: "+err.Error())
		return
	}
	if client == nil {
		writeError(w, 400, "未配置 CloudDrive2 API Token，请在设置页填写")
		return
	}
	if s.runner == nil {
		writeError(w, 500, "任务调度器未初始化")
		return
	}

	req.LocalDir = normalizePath(req.LocalDir)
	matchSize := s.cfg.Netdrive.MatchSize == nil || *s.cfg.Netdrive.MatchSize
	dedupeKey := "netdrive_sha1:" + strconv.FormatInt(req.LibraryID, 10) + ":" + req.RemotePath
	task, err := s.runner.Enqueue(repo.TaskKindNetdriveSha1,
		"CD2 补齐 SHA1: "+req.LocalDir, &dedupeKey, &req.LibraryID,
		repo.NetdriveSyncPayload{
			LibraryID:  req.LibraryID,
			LocalDir:   req.LocalDir,
			RemotePath: req.RemotePath,
			MatchSize:  matchSize,
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
