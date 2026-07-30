// handlers.go：HTTP API 请求处理器。
// 代码注释使用中文。
package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"memable/internal/media"
	"memable/internal/repo"
	"memable/internal/task"
)

// ===== 收藏库管理（阶段 8）=====

func (s *Server) handleListLibraries(w http.ResponseWriter, r *http.Request) {
	all, err := s.libraries.List()
	if err != nil {
		writeError(w, 500, "查询收藏库失败: "+err.Error())
		return
	}
	// 过滤掉 ID ≤ 0 的无效库记录（SQLite AUTOINCREMENT 从 1 开始）
	libs := make([]repo.Library, 0, len(all))
	for _, l := range all {
		if l.ID > 0 {
			libs = append(libs, l)
		}
	}
	writeJSON(w, 200, libs)
}

type createLibraryReq struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Kind string `json:"kind"` // image/video/mixed
}

func (s *Server) handleCreateLibrary(w http.ResponseWriter, r *http.Request) {
	var req createLibraryReq
	if err := parseJSON(r, &req); err != nil {
		writeError(w, 400, "请求体格式错误")
		return
	}
	if req.Name == "" || req.Path == "" {
		writeError(w, 400, "name 和 path 不能为空")
		return
	}
	if req.Kind == "" {
		req.Kind = "mixed"
	}
	lib := &repo.Library{Name: req.Name, Path: req.Path, Kind: req.Kind}
	if err := s.libraries.Create(lib); err != nil {
		writeError(w, 500, "创建收藏库失败: "+err.Error())
		return
	}
	writeJSON(w, 201, lib)
}

func (s *Server) handleGetLibrary(w http.ResponseWriter, r *http.Request) {
	id, err := parseInt64(r.PathValue("id"))
	if err != nil {
		writeError(w, 400, "无效的库 ID")
		return
	}
	lib, err := s.libraries.GetByID(id)
	if err != nil {
		writeError(w, 404, "收藏库不存在")
		return
	}
	writeJSON(w, 200, lib)
}

type updateLibraryReq struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Kind string `json:"kind"`
}

func (s *Server) handleUpdateLibrary(w http.ResponseWriter, r *http.Request) {
	id, err := parseInt64(r.PathValue("id"))
	if err != nil {
		writeError(w, 400, "无效的库 ID")
		return
	}
	var req updateLibraryReq
	if err := parseJSON(r, &req); err != nil {
		writeError(w, 400, "请求体格式错误")
		return
	}
	// 根目录迁移：仅 UPDATE path，相对路径不变
	if req.Path != "" {
		if err := s.libraries.UpdatePath(id, req.Path); err != nil {
			writeError(w, 500, "迁移路径失败: "+err.Error())
			return
		}
	}
	lib, _ := s.libraries.GetByID(id)
	writeJSON(w, 200, lib)
}

func (s *Server) handleDeleteLibrary(w http.ResponseWriter, r *http.Request) {
	id, err := parseInt64(r.PathValue("id"))
	if err != nil {
		writeError(w, 400, "无效的库 ID")
		return
	}

	// 数据库清理在同一事务中完成：删除会话、收藏库及级联关联媒体。
	thumbnailPaths, err := s.libraries.DeleteWithRelatedData(id)
	if err != nil {
		writeError(w, 500, "删除收藏库失败: "+err.Error())
		return
	}

	// 数据提交后，仅清理未被其他收藏库引用的缩略图文件。
	for _, thumbRel := range thumbnailPaths {
		n, err := s.media.CountThumbnailReferences(thumbRel)
		if err != nil {
			writeError(w, 500, "收藏库数据已删除，但检查缩略图引用失败: "+err.Error())
			return
		}
		if n > 0 {
			continue // 其他库仍在使用
		}
		thumbAbs := s.thumbAbsPath(thumbRel)
		if err := os.Remove(thumbAbs); err != nil && !os.IsNotExist(err) {
			writeError(w, 500, "收藏库数据已删除，但删除缩略图失败: "+err.Error())
			return
		}
		// 哈希分片目录为空时一并清理，失败不影响删除结果。
		_ = os.Remove(filepath.Dir(thumbAbs))
	}

	writeJSON(w, 200, map[string]string{"status": "deleted"})
}

// FileTreeNode 文件树节点。
type FileTreeNode struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	IsDir       bool   `json:"is_dir"`
	Size        int64  `json:"size,omitempty"`
	HasChildren bool   `json:"has_children,omitempty"`
}

func (s *Server) handleFileTree(w http.ResponseWriter, r *http.Request) {
	id, err := parseInt64(r.PathValue("id"))
	if err != nil {
		writeError(w, 400, "无效的库 ID")
		return
	}
	lib, err := s.libraries.GetByID(id)
	if err != nil {
		writeError(w, 404, "收藏库不存在")
		return
	}

	dir := r.URL.Query().Get("path")
	dir = normalizePath(dir)
	if dir == "." || dir == "/" || dir == "" {
		dir = ""
	}
	// 安全检查：不允许跳出库根目录
	if dir != "" && isUnsafePath(dir) {
		writeError(w, 400, "路径非法")
		return
	}
	nodes := listDirChildren(lib.Path, dir)
	writeJSON(w, 200, nodes)
}

// listDirChildren 列出指定目录的直属子项，目录节点仅检查是否有子项（has_children），不递归。
func listDirChildren(basePath, relPath string) []FileTreeNode {
	absPath := filepath.Join(basePath, relPath)
	entries, err := os.ReadDir(absPath)
	if err != nil {
		return []FileTreeNode{}
	}

	nodes := make([]FileTreeNode, 0, len(entries))
	for _, e := range entries {
		childRel := joinPath(relPath, e.Name())
		node := FileTreeNode{
			Name:  e.Name(),
			Path:  childRel,
			IsDir: e.IsDir(),
		}
		if e.IsDir() {
			// 检查子目录是否有子项（用于展开图标）
			subEntries, err := os.ReadDir(filepath.Join(basePath, childRel))
			node.HasChildren = err == nil && len(subEntries) > 0
		} else {
			info, _ := e.Info()
			if info != nil {
				node.Size = info.Size()
			}
		}
		nodes = append(nodes, node)
	}
	return nodes
}

// isUnsafePath 检查路径是否包含 .. 等危险成分。
func isUnsafePath(relPath string) bool {
	clean := filepath.ToSlash(relPath)
	if clean == ".." || clean == "." {
		return true
	}
	for _, part := range splitPath(clean) {
		if part == ".." || part == "." {
			return true
		}
	}
	return false
}

// splitPath 将正斜杠路径拆分为各段。
func splitPath(p string) []string {
	if p == "" {
		return nil
	}
	var parts []string
	for {
		dir, file := filepath.Split(p)
		if file == "" {
			break
		}
		parts = append([]string{file}, parts...)
		p = strings.TrimSuffix(dir, "/")
		if p == "" {
			break
		}
	}
	return parts
}

// normalizePath 将路径转为正斜杠并清理。
func normalizePath(p string) string {
	p = filepath.ToSlash(p)
	p = strings.TrimPrefix(p, "./")
	p = strings.TrimPrefix(p, "/")
	p = strings.TrimSuffix(p, "/")
	if p == "." {
		return ""
	}
	return p
}

// handleListFiles 列出库下指定目录的直属媒体（仅直接包含的文件，不含子目录）。
func (s *Server) handleListFiles(w http.ResponseWriter, r *http.Request) {
	id, err := parseInt64(r.PathValue("id"))
	if err != nil {
		writeError(w, 400, "无效的库 ID")
		return
	}
	dir := normalizePath(r.URL.Query().Get("path"))
	if isUnsafePath(dir) {
		writeError(w, 400, "路径非法")
		return
	}
	medias, err := s.media.ListByDirectoryDirect(id, dir)
	if err != nil {
		writeError(w, 500, "查询媒体列表失败: "+err.Error())
		return
	}
	if medias == nil {
		medias = []repo.Media{}
	}
	writeJSON(w, 200, medias)
}

// ===== 目录删除 =====

type deleteDirReq struct {
	Path string `json:"path"`
}

func (s *Server) handleDeleteDirectory(w http.ResponseWriter, r *http.Request) {
	id, err := parseInt64(r.PathValue("id"))
	if err != nil {
		writeError(w, 400, "无效的库 ID")
		return
	}
	lib, err := s.libraries.GetByID(id)
	if err != nil {
		writeError(w, 404, "收藏库不存在")
		return
	}

	var req deleteDirReq
	if err := parseJSON(r, &req); err != nil {
		writeError(w, 400, "请求体格式错误")
		return
	}
	req.Path = normalizePath(req.Path)
	if req.Path == "" {
		writeError(w, 400, "不能删除根目录")
		return
	}
	if isUnsafePath(req.Path) {
		writeError(w, 400, "路径非法")
		return
	}

	// 检查该库是否有活跃任务
	if active, _ := s.tasks.HasActiveForLibrary(id); active {
		writeError(w, 409, "该库有正在运行的任务，请稍后再试")
		return
	}

	// 验证目录存在
	absDir := filepath.Join(lib.Path, req.Path)
	info, err := os.Stat(absDir)
	if err != nil || !info.IsDir() {
		writeError(w, 404, "目录不存在")
		return
	}

	dedupeKey := fmt.Sprintf("dir_del:%d:%s", id, req.Path)
	libraryID := id
	task, err := s.runner.Enqueue(
		repo.TaskKindDirectoryDelete,
		"删除目录: "+filepath.Base(req.Path),
		&dedupeKey, &libraryID,
		map[string]any{"library_id": id, "dir_path": req.Path},
	)
	if err != nil {
		writeError(w, 509, "提交删除任务失败: "+err.Error())
		return
	}

	pos, _ := s.tasks.QueuePosition(task.ID)
	writeJSON(w, 202, map[string]any{
		"task_id":        task.ID,
		"queue_position": pos,
		"status":         task.Status,
	})
}

// ===== 扫描 =====

type scanReq struct {
	Force bool `json:"force"`
}

func (s *Server) handleScanLibrary(w http.ResponseWriter, r *http.Request) {
	id, err := parseInt64(r.PathValue("id"))
	if err != nil {
		writeError(w, 400, "无效的库 ID")
		return
	}
	lib, err := s.libraries.GetByID(id)
	if err != nil {
		writeError(w, 404, "收藏库不存在")
		return
	}
	var req scanReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		writeError(w, 400, "请求体格式错误")
		return
	}

	// 检查是否有活动任务
	if active, _ := s.tasks.HasActiveForLibrary(id); active {
		writeError(w, 409, "该库已有排队中或运行中的任务")
		return
	}

	dedupeKey := fmt.Sprintf("scan:%d", lib.ID)
	title := "同步扫描: " + lib.Name
	if req.Force {
		title = "强制同步扫描: " + lib.Name
	}
	task, err := s.runner.Enqueue(repo.TaskKindScan, title, &dedupeKey, &lib.ID,
		task.ScanPayload{LibraryPath: lib.Path, LibraryName: lib.Name, LibraryKind: lib.Kind, Force: req.Force})
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

type scanTempReq struct {
	Path string `json:"path"`
}

func (s *Server) handleScanTemporary(w http.ResponseWriter, r *http.Request) {
	var req scanTempReq
	if err := parseJSON(r, &req); err != nil || req.Path == "" {
		writeError(w, 400, "path 不能为空")
		return
	}

	dedupeKey := fmt.Sprintf("temp_scan:%s", filepath.Clean(req.Path))
	task, err := s.runner.Enqueue(repo.TaskKindTemporaryScan, "临时扫描: "+filepath.Base(req.Path),
		&dedupeKey, nil,
		task.ScanPayload{LibraryPath: req.Path, LibraryName: "临时扫描-" + filepath.Base(req.Path), LibraryKind: "mixed"})
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

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	session, err := s.sessions.GetByID(id)
	if err != nil {
		writeError(w, 404, "扫描会话不存在")
		return
	}
	medias, _ := s.media.ListBySession(id)
	writeJSON(w, 200, map[string]any{
		"session": session,
		"media":   medias,
		"count":   len(medias),
	})
}

func (s *Server) handleCancelSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.scanSvc.CancelScan(id); err != nil {
		writeError(w, 500, "取消扫描失败: "+err.Error())
		return
	}
	writeJSON(w, 200, map[string]string{"status": "cancelled"})
}

// handlePromoteSession 临时扫描入库：移动文件 + UPDATE is_temporary=0 + 迁移缩略图。
func (s *Server) handlePromoteSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	session, err := s.sessions.GetByID(id)
	if err != nil {
		writeError(w, 404, "扫描会话不存在")
		return
	}
	if !session.IsTemporary {
		writeError(w, 400, "该会话不是临时扫描，无需入库")
		return
	}

	var req struct {
		LibraryID int64 `json:"library_id"`
	}
	if err := parseJSON(r, &req); err != nil {
		writeError(w, 400, "请求体格式错误")
		return
	}

	lib, err := s.libraries.GetByID(req.LibraryID)
	if err != nil {
		writeError(w, 404, "目标收藏库不存在")
		return
	}

	// 临时扫描的源路径来自关联的库记录
	var srcBasePath string
	if session.LibraryID != nil {
		srcLib, err := s.libraries.GetByID(*session.LibraryID)
		if err == nil {
			srcBasePath = srcLib.Path
		}
	}

	// 查出该会话的全部媒体
	medias, err := s.media.ListBySession(id)
	if err != nil {
		writeError(w, 500, "查询媒体失败: "+err.Error())
		return
	}

	moved := 0
	for _, m := range medias {
		// 移动源文件到目标收藏库
		if srcBasePath != "" {
			srcPath := filepath.Join(srcBasePath, m.RelativePath)
			dstPath := filepath.Join(lib.Path, m.RelativePath)
			if err := moveFile(srcPath, dstPath); err != nil {
				slog.Warn("移动文件失败", "src", srcPath, "dst", dstPath, "err", err)
			} else {
				moved++
			}
		}

		// 缩略图已使用内容寻址路径，无需迁移
		// 更新 media 记录的 library_id
		if err := s.media.UpdateLibrary(m.ID, req.LibraryID); err != nil {
			slog.Error("更新媒体库归属失败", "media_id", m.ID, "err", err)
		}
	}

	// 标记会话为已入库
	if err := s.sessions.Promote(id); err != nil {
		writeError(w, 500, "入库标记失败: "+err.Error())
		return
	}

	// 删除临时库记录（媒体已迁移到目标库）
	if session.LibraryID != nil {
		_ = s.libraries.Delete(*session.LibraryID)
	}

	writeJSON(w, 200, map[string]any{
		"status":  "promoted",
		"moved":   moved,
		"library": lib.Name,
	})
}

// ===== 搜索（阶段 7）=====

func (s *Server) handleSearchText(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		writeError(w, 400, "查询参数 q 不能为空")
		return
	}
	results, err := s.searchSvc.SearchByText(q)
	if err != nil {
		writeError(w, 500, "搜索失败: "+err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{
		"query":   q,
		"results": results,
		"count":   len(results),
	})
}

type searchImageReq struct {
	Phash       string `json:"phash"`
	MaxDistance int    `json:"max_distance"`
}

func (s *Server) handleSearchImage(w http.ResponseWriter, r *http.Request) {
	var req searchImageReq
	if err := parseJSON(r, &req); err != nil || req.Phash == "" {
		writeError(w, 400, "phash 不能为空")
		return
	}
	results, err := s.searchSvc.SearchByImage(req.Phash, req.MaxDistance)
	if err != nil {
		writeError(w, 500, "以图搜图失败: "+err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{
		"results": results,
		"count":   len(results),
	})
}

func (s *Server) handleSearchImageUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 32<<20) // 32 MB 上限
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		writeError(w, 400, "解析上传失败: "+err.Error())
		return
	}

	file, _, err := r.FormFile("image")
	if err != nil {
		writeError(w, 400, "缺少 image 字段")
		return
	}
	defer file.Close()

	tmpFile, err := os.CreateTemp("", "search-upload-*.png")
	if err != nil {
		writeError(w, 500, "创建临时文件失败")
		return
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	if _, err := io.Copy(tmpFile, file); err != nil {
		writeError(w, 500, "写入临时文件失败")
		return
	}
	tmpFile.Close()

	hashes, err := media.ImagePerceptualHashes(tmpFile.Name())
	if err != nil {
		writeError(w, 500, "计算 pHash 失败: "+err.Error())
		return
	}

	results, err := s.searchSvc.SearchByImage(hashes.PHash, 12)
	if err != nil {
		writeError(w, 500, "以图搜图失败: "+err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{
		"results": results,
		"count":   len(results),
	})
}

// ===== 重复报告（阶段 6）=====

type reportReq struct {
	OutputPath string `json:"output_path"`
}

func (s *Server) handleImageReport(w http.ResponseWriter, r *http.Request) {
	var req reportReq
	if err := parseJSON(r, &req); err != nil || req.OutputPath == "" {
		req.OutputPath = "report_image.html"
	}

	dedupeKey := fmt.Sprintf("report_image:%s", req.OutputPath)
	task, err := s.runner.Enqueue(repo.TaskKindReportImage, "图片重复报告", &dedupeKey, nil,
		task.ReportPayload{OutputPath: req.OutputPath})
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

func (s *Server) handleVideoReport(w http.ResponseWriter, r *http.Request) {
	var req reportReq
	if err := parseJSON(r, &req); err != nil || req.OutputPath == "" {
		req.OutputPath = "report_video.html"
	}

	dedupeKey := fmt.Sprintf("report_video:%s", req.OutputPath)
	t, err := s.runner.Enqueue(repo.TaskKindReportVideo, "视频重复报告", &dedupeKey, nil,
		task.ReportPayload{OutputPath: req.OutputPath})
	if err != nil {
		writeError(w, 409, "相同任务已在等待或执行")
		return
	}

	pos, _ := s.tasks.QueuePosition(t.ID)
	writeJSON(w, 202, map[string]any{
		"task_id":        t.ID,
		"status":         t.Status,
		"queue_position": pos,
	})
}

// ===== 缩略图静态服务 =====

func (s *Server) handleThumbnail(w http.ResponseWriter, r *http.Request) {
	relPath := r.URL.Path[len("/api/thumbnails/"):]
	absPath := filepath.Join(s.thumbBase, relPath)
	w.Header().Set("Content-Type", "image/png")
	http.ServeFile(w, r, absPath)
}

// ===== 健康检查 =====

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

// ===== 任务管理 =====

func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) {
	tasks, err := s.tasks.ListAll(100, 0)
	if err != nil {
		writeError(w, 500, "查询任务列表失败: "+err.Error())
		return
	}
	// 计算排队位置
	for i := range tasks {
		if tasks[i].Status == repo.TaskStatusQueued {
			pos, _ := s.tasks.QueuePosition(tasks[i].ID)
			tasks[i].QueuePosition = pos
		}
	}
	writeJSON(w, 200, tasks)
}

func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	task, err := s.tasks.GetByID(id)
	if err != nil {
		writeError(w, 404, "任务不存在")
		return
	}
	if task.Status == repo.TaskStatusQueued {
		pos, _ := s.tasks.QueuePosition(task.ID)
		task.QueuePosition = pos
	}
	writeJSON(w, 200, task)
}

func (s *Server) handleCancelTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.runner.CancelTask(id); err != nil {
		writeError(w, 500, "取消任务失败: "+err.Error())
		return
	}
	writeJSON(w, 200, map[string]string{"status": "cancelled"})
}

// ===== 工具 - 文件统计 =====

type createFileStatsReq struct {
	DirPath string `json:"dir_path"`
}

func (s *Server) handleCreateFileStats(w http.ResponseWriter, r *http.Request) {
	var req createFileStatsReq
	if err := parseJSON(r, &req); err != nil {
		writeError(w, 400, "请求体格式错误")
		return
	}
	if req.DirPath == "" {
		writeError(w, 400, "dir_path 不能为空")
		return
	}

	info, err := os.Stat(req.DirPath)
	if err != nil || !info.IsDir() {
		writeError(w, 400, "目录不存在或不是有效目录")
		return
	}

	result, tree := walkDirStats(req.DirPath)
	if result == nil {
		writeError(w, 500, "遍历目录失败")
		return
	}

	extStatsJSON, _ := json.Marshal(result.extStats)
	treeJSON, _ := json.Marshal(tree)

	fs := &repo.FileStats{
		DirPath:    req.DirPath,
		TotalBytes: result.totalBytes,
		TotalCount: result.totalCount,
		ExtStats:   string(extStatsJSON),
		FileTree:   string(treeJSON),
	}
	if err := s.fileStats.Create(fs); err != nil {
		writeError(w, 500, "保存统计记录失败: "+err.Error())
		return
	}

	writeJSON(w, 201, fs)
}

func (s *Server) handleListFileStats(w http.ResponseWriter, r *http.Request) {
	fsList, err := s.fileStats.List(50, 0)
	if err != nil {
		writeError(w, 500, "查询统计记录失败: "+err.Error())
		return
	}
	if fsList == nil {
		fsList = []repo.FileStats{}
	}
	writeJSON(w, 200, fsList)
}

func (s *Server) handleGetFileStats(w http.ResponseWriter, r *http.Request) {
	id, err := parseInt64(r.PathValue("id"))
	if err != nil {
		writeError(w, 400, "无效的记录 ID")
		return
	}
	fs, err := s.fileStats.GetByID(id)
	if err != nil {
		writeError(w, 404, "统计记录不存在")
		return
	}
	writeJSON(w, 200, fs)
}

func (s *Server) handleDeleteFileStats(w http.ResponseWriter, r *http.Request) {
	id, err := parseInt64(r.PathValue("id"))
	if err != nil {
		writeError(w, 400, "无效的记录 ID")
		return
	}
	if err := s.fileStats.Delete(id); err != nil {
		writeError(w, 404, "统计记录不存在")
		return
	}
	writeJSON(w, 200, map[string]string{"status": "deleted"})
}
