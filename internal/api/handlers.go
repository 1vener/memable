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
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"memable/internal/media"
	"memable/internal/recycle"
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
	thumbnailRefs, err := s.libraries.DeleteWithRelatedData(id)
	if err != nil {
		writeError(w, 500, "删除收藏库失败: "+err.Error())
		return
	}

	// 数据提交后，仅清理未被其他收藏库引用的缩略图文件。
	for _, ref := range thumbnailRefs {
		n, err := s.media.CountThumbnailReferences(ref.Rel)
		if err != nil {
			writeError(w, 500, "收藏库数据已删除，但检查缩略图引用失败: "+err.Error())
			return
		}
		if n > 0 {
			continue // 其他库仍在使用
		}
		thumbAbs := s.thumbAbsPath(ref.Kind, ref.Rel)
		if err := os.Remove(thumbAbs); err != nil && !os.IsNotExist(err) {
			writeError(w, 500, "收藏库数据已删除，但删除缩略图失败: "+err.Error())
			return
		}
		// 哈希分片目录为空时一并清理，失败不影响删除结果。
		_ = os.Remove(filepath.Dir(thumbAbs))
	}

	// 收藏库删除后维护重复报告（media 级联删除成员 → 清理 <2 的组 → 刷新统计）
	if s.dup != nil {
		if err := s.dup.PruneAfterMediaChange(); err != nil {
			writeError(w, 500, "收藏库数据已删除，但刷新重复报告失败: "+err.Error())
			return
		}
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

	// 同步删除（不提交后台任务），客户端可直接拿到结果并刷新树节点。
	// 阶段1：删除数据库媒体记录，收集缩略图引用
	deletedMedia, thumbRefs, err := s.media.DeleteByDirectory(id, req.Path)
	if err != nil {
		writeError(w, 500, "删除数据库记录失败: "+err.Error())
		return
	}

	// 阶段2：删除本地目录（按 delete.permanent 配置：永久删除或移入回收站）
	var dirErr error
	if s.cfg.Delete.Permanent {
		dirErr = os.RemoveAll(absDir)
	} else {
		dirErr = recycle.ToBinDir(absDir)
	}
	localDeleted := dirErr == nil || os.IsNotExist(dirErr)
	if dirErr != nil && !os.IsNotExist(dirErr) {
		slog.Warn("删除本地目录失败（数据库已清理）", "path", absDir, "err", dirErr)
	}

	// 阶段3：清理无引用缩略图
	deletedThumbs := 0
	for _, ref := range thumbRefs {
		n, err := s.media.CountThumbnailReferences(ref.Rel)
		if err != nil || n > 0 {
			continue
		}
		thumbAbs := s.thumbAbsPath(ref.Kind, ref.Rel)
		if err := os.Remove(thumbAbs); err != nil && !os.IsNotExist(err) {
			slog.Warn("删除缩略图失败", "path", thumbAbs, "err", err)
			continue
		}
		_ = os.Remove(filepath.Dir(thumbAbs))
		deletedThumbs++
	}

	// 阶段4：维护重复报告（级联清理不足 2 个成员的组并更新统计）
	if s.dup != nil {
		if err := s.dup.PruneAfterMediaChange(); err != nil {
			slog.Warn("目录删除后刷新重复报告失败", "err", err)
		}
	}

	writeJSON(w, 200, map[string]any{
		"status":         "deleted",
		"deleted_media":  deletedMedia,
		"deleted_thumbs": deletedThumbs,
		"local_deleted":  localDeleted,
		"dir_path":       req.Path,
	})
}

// ===== 目录重命名 / 移动 =====

type renameDirReq struct {
	Path    string `json:"path"`
	NewName string `json:"new_name"`
}

// validDirName 校验目录名合法：非空、不含路径分隔与 Windows 非法字符、非 . / ..。
func validDirName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	return !strings.ContainsAny(name, `/\:*?"<>|`)
}

// splitRelPath 将相对路径拆分为父目录与末级名（如 "a/b/c" → ("a/b", "c")）。
func splitRelPath(rel string) (parent, base string) {
	rel = strings.ReplaceAll(rel, "\\", "/")
	idx := strings.LastIndex(rel, "/")
	if idx < 0 {
		return "", rel
	}
	return rel[:idx], rel[idx+1:]
}

// handleRenameDirectory 重命名库内目录：本地改名 + 批量更新 media.relative_path 前缀。
// 顺序：先改盘（原子 rename），成功后改库；库更新失败时回滚移回。
func (s *Server) handleRenameDirectory(w http.ResponseWriter, r *http.Request) {
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

	var req renameDirReq
	if err := parseJSON(r, &req); err != nil {
		writeError(w, 400, "请求体格式错误")
		return
	}
	oldPath := normalizePath(req.Path)
	if oldPath == "" {
		writeError(w, 400, "不能重命名根目录")
		return
	}
	if isUnsafePath(oldPath) {
		writeError(w, 400, "路径非法")
		return
	}
	newName := strings.TrimSpace(req.NewName)
	if !validDirName(newName) {
		writeError(w, 400, "目录名非法（不能包含 /\\:*?\"<>| 等字符）")
		return
	}
	parent, base := splitRelPath(oldPath)
	if base == "" {
		writeError(w, 400, "不能重命名根目录")
		return
	}
	if base == newName {
		writeError(w, 400, "目录名未变化")
		return
	}
	newPath := joinRel(parent, newName)
	// Windows/大小写不敏感文件系统：仅改大小写时新路径与源在文件系统上等价，
	// os.Stat 必然命中自身，不能做"目标已存在"检查。
	caseOnly := strings.EqualFold(base, newName) && base != newName

	// 检查该库是否有活跃任务
	if active, _ := s.tasks.HasActiveForLibrary(id); active {
		writeError(w, 409, "该库有正在运行的任务，请稍后再试")
		return
	}

	absOld := filepath.Join(lib.Path, oldPath)
	info, err := os.Stat(absOld)
	if err != nil || !info.IsDir() {
		writeError(w, 404, "目录不存在")
		return
	}
	absNew := filepath.Join(lib.Path, newPath)
	if !caseOnly {
		if _, err := os.Stat(absNew); err == nil {
			writeError(w, 409, "目标目录「"+newName+"」已存在")
			return
		}
	}

	// 磁盘改名。Windows 大小写改名（A → a）时目标路径与源文件系统等价，
	// 直接 rename 会失败或无效果：先改到临时名再改最终名。
	if caseOnly {
		tmpName := fmt.Sprintf(".%s.ren-%d", base, time.Now().UnixNano())
		tmpAbs := filepath.Join(filepath.Dir(absOld), tmpName)
		if err := os.Rename(absOld, tmpAbs); err != nil {
			writeError(w, 500, "改名失败: "+err.Error())
			return
		}
		if err := os.Rename(tmpAbs, absNew); err != nil {
			_ = os.Rename(tmpAbs, absOld) // 回滚临时名
			writeError(w, 500, "改名失败: "+err.Error())
			return
		}
	} else {
		if err := os.Rename(absOld, absNew); err != nil {
			writeError(w, 500, "改名失败: "+err.Error())
			return
		}
	}

	// 批量更新数据库相对路径前缀；失败时回滚磁盘
	n, err := s.media.RenameDirectoryPrefix(id, oldPath, newPath)
	if err != nil {
		slog.Error("重命名目录后更新数据库失败，回滚磁盘", "old", oldPath, "new", newPath, "err", err)
		if rbErr := os.Rename(absNew, absOld); rbErr != nil {
			slog.Error("回滚磁盘改名失败，请手动检查", "path", absNew, "err", rbErr)
		}
		writeError(w, 500, "更新数据库失败: "+err.Error())
		return
	}

	slog.Info("重命名目录完成", "library", lib.Name, "old", oldPath, "new", newPath, "media", n)
	writeJSON(w, 200, map[string]any{
		"status":        "renamed",
		"renamed_media": n,
		"old_path":      oldPath,
		"new_path":      newPath,
		"old_abs":       absOld,
		"new_abs":       absNew,
	})
}

type moveDirReq struct {
	Path      string `json:"path"`
	TargetDir string `json:"target_dir"`
}

// handleMoveDirectory 移动库内目录到指定目录下（结果 target_dir/末级名）：
// 本地移动 + 批量更新 media.relative_path 前缀。
// 仅支持同卷移动（os.Rename）；跨卷/失败直接报错，不做递归复制。
func (s *Server) handleMoveDirectory(w http.ResponseWriter, r *http.Request) {
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

	var req moveDirReq
	if err := parseJSON(r, &req); err != nil {
		writeError(w, 400, "请求体格式错误")
		return
	}
	srcPath := normalizePath(req.Path)
	if srcPath == "" {
		writeError(w, 400, "不能移动根目录")
		return
	}
	if isUnsafePath(srcPath) {
		writeError(w, 400, "路径非法")
		return
	}
	targetDir := normalizeRelDir(req.TargetDir)
	if isUnsafePath(targetDir) {
		writeError(w, 400, "目标目录非法")
		return
	}
	// 禁止移动到自身或其子孙目录
	if targetDir == srcPath || strings.HasPrefix(targetDir, srcPath+"/") {
		writeError(w, 400, "不能移动到自身或其子目录")
		return
	}
	_, base := splitRelPath(srcPath)
	newPath := joinRel(targetDir, base)
	if newPath == srcPath {
		writeError(w, 400, "目录已在目标位置")
		return
	}

	// 检查该库是否有活跃任务
	if active, _ := s.tasks.HasActiveForLibrary(id); active {
		writeError(w, 409, "该库有正在运行的任务，请稍后再试")
		return
	}

	absOld := filepath.Join(lib.Path, srcPath)
	info, err := os.Stat(absOld)
	if err != nil || !info.IsDir() {
		writeError(w, 404, "目录不存在")
		return
	}
	absNew := filepath.Join(lib.Path, newPath)
	if _, err := os.Stat(absNew); err == nil {
		writeError(w, 409, "目标目录「"+base+"」已存在")
		return
	}

	// 本地移动（先确保目标父目录存在）
	if err := os.MkdirAll(filepath.Dir(absNew), 0o755); err != nil {
		writeError(w, 500, "创建目标目录失败: "+err.Error())
		return
	}
	if err := os.Rename(absOld, absNew); err != nil {
		writeError(w, 500, "移动目录失败（同卷移动，跨卷或占用时无法完成）: "+err.Error())
		return
	}

	// 批量更新数据库相对路径前缀；失败时回滚磁盘
	n, err := s.media.RenameDirectoryPrefix(id, srcPath, newPath)
	if err != nil {
		slog.Error("移动目录后更新数据库失败，回滚磁盘", "old", srcPath, "new", newPath, "err", err)
		if rbErr := os.Rename(absNew, absOld); rbErr != nil {
			slog.Error("回滚磁盘移动失败，请手动检查", "path", absNew, "err", rbErr)
		}
		writeError(w, 500, "更新数据库失败: "+err.Error())
		return
	}

	slog.Info("移动目录完成", "library", lib.Name, "old", srcPath, "new", newPath, "media", n)
	writeJSON(w, 200, map[string]any{
		"status":      "moved",
		"moved_media": n,
		"old_path":    srcPath,
		"new_path":    newPath,
		"old_abs":     absOld,
		"new_abs":     absNew,
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

// handleScanSha1 启动补齐 SHA1 后台任务（主扫描不生成视频 SHA1，需要时单独补齐）。
func (s *Server) handleScanSha1(w http.ResponseWriter, r *http.Request) {
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

	// 检查是否有活动任务
	if active, _ := s.tasks.HasActiveForLibrary(id); active {
		writeError(w, 409, "该库已有排队中或运行中的任务")
		return
	}

	dedupeKey := fmt.Sprintf("scan_sha1:%d", lib.ID)
	task, err := s.runner.Enqueue(repo.TaskKindScanSha1, "补齐 SHA1: "+lib.Name, &dedupeKey, &lib.ID, nil)
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

// promoteLibraryReq 库维度入库请求。
type promoteLibraryReq struct {
	TargetLibraryID int64  `json:"target_library_id"`
	TargetDir       string `json:"target_dir"` // 目标库下子目录（相对，空=库根）
}

// handlePromoteLibrary 将临时扫描库的媒体移动到正式收藏库的指定目录下：
// 移动本地文件 + 同步更新 media.library_id/relative_path。
// 目标库存在相同相对路径时，以"临时库最后一级目录名(1)"作为子目录段递增避让。
func (s *Server) handlePromoteLibrary(w http.ResponseWriter, r *http.Request) {
	srcID, err := parseInt64(r.PathValue("id"))
	if err != nil {
		writeError(w, 400, "无效的库 ID")
		return
	}
	srcLib, err := s.libraries.GetByID(srcID)
	if err != nil {
		writeError(w, 404, "收藏库不存在")
		return
	}
	if !srcLib.IsTemporary {
		writeError(w, 400, "该库不是临时扫描库")
		return
	}
	session, err := s.sessions.GetLatestTemporaryByLibrary(srcID)
	if err != nil {
		writeError(w, 500, "查询临时会话失败: "+err.Error())
		return
	}
	if session == nil {
		writeError(w, 400, "该临时库没有可入库的扫描会话")
		return
	}

	var req promoteLibraryReq
	if err := parseJSON(r, &req); err != nil {
		writeError(w, 400, "请求体格式错误")
		return
	}
	if req.TargetLibraryID <= 0 || req.TargetLibraryID == srcID {
		writeError(w, 400, "目标收藏库无效")
		return
	}
	targetLib, err := s.libraries.GetByID(req.TargetLibraryID)
	if err != nil {
		writeError(w, 404, "目标收藏库不存在")
		return
	}
	if targetLib.IsTemporary {
		writeError(w, 400, "目标收藏库不能是临时扫描库")
		return
	}
	targetDir := normalizeRelDir(req.TargetDir)
	if targetDir == "" {
		targetDir = ""
	}

	medias, err := s.media.ListBySession(session.ID)
	if err != nil {
		writeError(w, 500, "查询媒体失败: "+err.Error())
		return
	}
	if len(medias) == 0 {
		writeError(w, 400, "临时扫描无媒体可入库")
		return
	}

	// 计算目标相对路径前缀：目标目录下以"临时库最后一级目录名"作为子目录段
	// （如 D:\tmp\test → 目标 D:\本地\2026 → D:\本地\2026\test\...）。
	// 与目标库已有路径冲突时递增避让为 名(1)、名(2)…
	baseName := filepath.Base(filepath.Clean(srcLib.Path))
	if baseName == "" || baseName == "." || baseName == string(filepath.Separator) {
		baseName = srcLib.Name
	}
	prefix := joinRel(targetDir, baseName)
	relPaths := make([]string, 0, len(medias))
	for _, m := range medias {
		relPaths = append(relPaths, joinRel(prefix, m.RelativePath))
	}
	for attempt := 1; ; attempt++ {
		conflict, err := s.media.HasAnyRelativePath(targetLib.ID, relPaths)
		if err != nil {
			writeError(w, 500, "检查路径冲突失败: "+err.Error())
			return
		}
		if !conflict {
			break
		}
		if attempt > 99 {
			writeError(w, 500, "路径冲突无法避让（已达上限）")
			return
		}
		prefix = joinRel(targetDir, fmt.Sprintf("%s(%d)", baseName, attempt))
		for i, m := range medias {
			relPaths[i] = joinRel(prefix, m.RelativePath)
		}
	}

	// 移动本地文件 + 更新媒体归属与相对路径
	moved := 0
	failed := 0
	for i, m := range medias {
		srcPath := filepath.Join(srcLib.Path, filepath.FromSlash(m.RelativePath))
		dstPath := filepath.Join(targetLib.Path, filepath.FromSlash(relPaths[i]))
		if err := moveFile(srcPath, dstPath); err != nil {
			slog.Warn("移动文件失败", "src", srcPath, "dst", dstPath, "err", err)
			failed++
			continue
		}
		if err := s.media.UpdateLibraryAndPath(m.ID, targetLib.ID, relPaths[i]); err != nil {
			slog.Error("更新媒体归属与路径失败", "media_id", m.ID, "err", err)
			failed++
			continue
		}
		moved++
	}

	// 标记会话已入库并删除临时库记录
	_ = s.sessions.Promote(session.ID)
	_ = s.libraries.Delete(srcID)

	slog.Info("临时库入库完成", "src_library", srcLib.Name, "target", targetLib.Name,
		"target_dir", prefix, "moved", moved, "failed", failed)
	writeJSON(w, 200, map[string]any{
		"moved":            moved,
		"failed":           failed,
		"library":          targetLib.Name,
		"target_dir":       prefix,
		"conflict_renamed": prefix != targetDir,
	})
}

// normalizeRelDir 归一化相对目录（正斜杠、去首尾斜杠）；非法路径（绝对/越界）返回空。
func normalizeRelDir(dir string) string {
	dir = strings.ReplaceAll(strings.TrimSpace(dir), "\\", "/")
	dir = strings.Trim(dir, "/")
	if dir == "" || dir == "." {
		return ""
	}
	for _, part := range strings.Split(dir, "/") {
		if part == ".." || part == "." || part == "" {
			return ""
		}
	}
	return dir
}

// joinRel 拼接相对路径前缀与文件相对路径（正斜杠；prefix 空时返回 rel）。
func joinRel(prefix, rel string) string {
	rel = strings.ReplaceAll(rel, "\\", "/")
	if prefix == "" {
		return rel
	}
	return prefix + "/" + rel
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

// ===== 媒体操作（打开文件/目录）=====

type openMediaReq struct {
	Action string `json:"action"` // "file" 或 "directory"
}

func (s *Server) handleOpenMedia(w http.ResponseWriter, r *http.Request) {
	id, err := parseInt64(r.PathValue("id"))
	if err != nil {
		writeError(w, 400, "无效的媒体 ID")
		return
	}

	var req openMediaReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "请求体格式错误")
		return
	}
	if req.Action != "file" && req.Action != "directory" {
		writeError(w, 400, "action 必须是 file 或 directory")
		return
	}

	// 1. 查询媒体记录
	m, err := s.media.GetByID(id)
	if err != nil {
		writeError(w, 500, "查询媒体失败: "+err.Error())
		return
	}
	if m == nil {
		writeError(w, 404, "媒体不存在")
		return
	}

	// 2. 查询所属收藏库
	lib, err := s.libraries.GetByID(m.LibraryID)
	if err != nil {
		writeError(w, 500, "查询收藏库失败: "+err.Error())
		return
	}
	if lib == nil {
		writeError(w, 404, "收藏库不存在")
		return
	}

	// 3. 构造并校验完整路径
	fullPath := filepath.Join(lib.Path, filepath.FromSlash(m.RelativePath))
	libAbs, err := filepath.Abs(lib.Path)
	if err != nil {
		writeError(w, 500, "解析库路径失败")
		return
	}
	fileAbs, err := filepath.Abs(fullPath)
	if err != nil {
		writeError(w, 500, "解析文件路径失败")
		return
	}
	// 安全校验：文件必须在收藏库根目录内
	rel, err := filepath.Rel(libAbs, fileAbs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		writeError(w, 403, "文件路径越界")
		return
	}
	if _, err := os.Stat(fileAbs); err != nil {
		writeError(w, 404, "文件已不存在")
		return
	}

	// 4. 跨平台执行打开命令
	slog.Info("打开系统文件/目录", "media_id", id, "action", req.Action, "path", fileAbs)
	if err := openFile(req.Action, fileAbs); err != nil {
		slog.Error("打开系统文件/目录失败", "media_id", id, "action", req.Action, "path", fileAbs, "err", err)
		writeError(w, 500, "打开失败: "+err.Error())
		return
	}

	writeJSON(w, 200, map[string]string{"status": "ok"})
}

// openFile 跨平台打开文件或文件所在目录；
// 打开目录时在文件管理器中选中该文件，方便一眼定位。
func openFile(action, absPath string) error {
	switch runtime.GOOS {
	case "windows":
		if action == "directory" {
			// 用 ShellExecuteW 打开目录并选中文件（参数 /select,"<path>"，
			// 引号只包路径；os/exec 的整体引号形式 explorer 不识别，详见 reveal_windows.go）。
			return revealInExplorer(absPath)
		}
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", absPath).Start()
	case "darwin":
		if action == "directory" {
			// open -R 在 Finder 中显示文件所在目录并选中该文件
			return exec.Command("open", "-R", absPath).Start()
		}
		return exec.Command("open", absPath).Start()
	case "linux":
		if action == "directory" {
			// 常见文件管理器支持 --select 打开目录并选中文件；未安装时回退为仅打开目录
			for _, cmd := range [][]string{
				{"nautilus", "--select", absPath},
				{"dolphin", "--select", absPath},
			} {
				if err := exec.Command(cmd[0], cmd[1:]...).Start(); err == nil {
					return nil
				}
			}
			return exec.Command("xdg-open", filepath.Dir(absPath)).Start()
		}
		return exec.Command("xdg-open", absPath).Start()
	default:
		return fmt.Errorf("不支持的平台: %s", runtime.GOOS)
	}
}

// selectArgs 构造 explorer 的选中参数：/select,"<path>"。
// 引号只包路径（explorer 唯一可靠形式，ShellExecuteW 直接透传不做转义）；
// Windows 文件名不允许包含引号，防御性剔除。
func selectArgs(absPath string) string {
	return `/select,"` + strings.ReplaceAll(absPath, `"`, "") + `"`
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

func (s *Server) handleImageReport(w http.ResponseWriter, r *http.Request) {
	dedupeKey := "report_image"
	task, err := s.runner.Enqueue(repo.TaskKindReportImage, "图片重复统计", &dedupeKey, nil, nil)
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
	dedupeKey := "report_video"
	t, err := s.runner.Enqueue(repo.TaskKindReportVideo, "视频重复统计", &dedupeKey, nil, nil)
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
	kind := r.PathValue("kind")
	if kind != "image" && kind != "video" {
		http.NotFound(w, r)
		return
	}
	prefix := "/api/thumbnails/" + kind + "/"
	relPath := strings.TrimPrefix(r.URL.Path, prefix)
	absPath := s.thumbAbsPath(kind, relPath)
	w.Header().Set("Content-Type", "image/png")
	http.ServeFile(w, r, absPath)
}

// ===== 健康检查 =====

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	resp := map[string]interface{}{"status": "ok"}
	if s.ffmpegCaps != nil {
		resp["ffmpeg"] = s.ffmpegCaps
	}
	if s.ffprobeCaps != nil {
		resp["ffprobe"] = s.ffprobeCaps
	}
	writeJSON(w, 200, resp)
}

// handleSettings 返回服务端存储位置（设置页展示用）。
func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	dbPath, _ := filepath.Abs(s.cfg.Database.Path)
	imageDir, _ := filepath.Abs(s.cfg.ImageThumbDir())
	videoDir, _ := filepath.Abs(s.cfg.VideoThumbDir())
	logFile := ""
	if strings.TrimSpace(s.cfg.Log.File) != "" {
		logFile, _ = filepath.Abs(s.cfg.Log.File)
	}
	writeJSON(w, 200, map[string]any{
		"database_path":       dbPath,   // 数据库文件路径
		"thumbnail_image_dir": imageDir, // 图片缩略图保存目录
		"thumbnail_video_dir": videoDir, // 视频封面保存目录
		"log_file":            logFile,  // 日志文件路径；空=输出到控制台
	})
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

// handleFileStatsDiff 对比历史统计与目录当前状态，返回新增/删除文件列表。
func (s *Server) handleFileStatsDiff(w http.ResponseWriter, r *http.Request) {
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
	if info, err := os.Stat(fs.DirPath); err != nil || !info.IsDir() {
		writeError(w, 400, "统计目录不存在或已不可访问")
		return
	}
	diff, err := computeFileDiff(fs.FileTree, fs.DirPath)
	if err != nil {
		writeError(w, 500, "对比目录差异失败: "+err.Error())
		return
	}
	writeJSON(w, 200, diff)
}

// handleFileStatsDiffExport 导出目录差异为 xlsx（两个 sheet：新增/删除文件列表）。
func (s *Server) handleFileStatsDiffExport(w http.ResponseWriter, r *http.Request) {
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
	if info, err := os.Stat(fs.DirPath); err != nil || !info.IsDir() {
		writeError(w, 400, "统计目录不存在或已不可访问")
		return
	}
	diff, err := computeFileDiff(fs.FileTree, fs.DirPath)
	if err != nil {
		writeError(w, 500, "对比目录差异失败: "+err.Error())
		return
	}
	// Excel 中写入完整绝对路径（正斜杠），便于直接定位文件
	added := make([]string, 0, len(diff.Added))
	for _, p := range diff.Added {
		added = append(added, absolutePath(fs.DirPath, p))
	}
	removed := make([]string, 0, len(diff.Removed))
	for _, p := range diff.Removed {
		removed = append(removed, absolutePath(fs.DirPath, p))
	}
	data, err := exportDiffXLSX(added, removed)
	if err != nil {
		writeError(w, 500, "生成 Excel 失败: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", `attachment; filename="file_diff.xlsx"`)
	w.WriteHeader(200)
	_, _ = w.Write(data)
}
