// handlers.go：HTTP API 请求处理器。
// 代码注释使用中文。
package api

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"memable/internal/duplicate"
	"memable/internal/media"
	"memable/internal/repo"
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

	// 先查出关联媒体，物理删除缩略图
	medias, _ := s.media.ListByLibrary(id)
	for _, m := range medias {
		if m.ThumbnailPath != nil {
			thumbAbs := s.thumbAbsPath(*m.ThumbnailPath)
			_ = os.Remove(thumbAbs)
		}
	}

	if err := s.libraries.Delete(id); err != nil {
		writeError(w, 500, "删除收藏库失败: "+err.Error())
		return
	}
	writeJSON(w, 200, map[string]string{"status": "deleted"})
}

// FileTreeNode 文件树节点。
type FileTreeNode struct {
	Name     string         `json:"name"`
	Path     string         `json:"path"`
	IsDir    bool           `json:"is_dir"`
	Size     int64          `json:"size,omitempty"`
	Children []FileTreeNode `json:"children,omitempty"`
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

	tree := buildFileTree(lib.Path, "")
	writeJSON(w, 200, tree)
}

// buildFileTree 递归构建文件树。
func buildFileTree(basePath, relPath string) []FileTreeNode {
	absPath := filepath.Join(basePath, relPath)
	entries, err := os.ReadDir(absPath)
	if err != nil {
		return nil
	}

	var nodes []FileTreeNode
	for _, e := range entries {
		childRel := joinPath(relPath, e.Name())
		node := FileTreeNode{
			Name:  e.Name(),
			Path:  childRel,
			IsDir: e.IsDir(),
		}
		if !e.IsDir() {
			info, _ := e.Info()
			if info != nil {
				node.Size = info.Size()
			}
		} else {
			node.Children = buildFileTree(basePath, childRel)
		}
		nodes = append(nodes, node)
	}
	return nodes
}

// ===== 扫描 =====

type scanReq struct {
	Temporary bool `json:"temporary"`
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

	poolSize := 4
	if s.cfg != nil && s.cfg.Worker.PoolSize > 0 {
		poolSize = s.cfg.Worker.PoolSize
	}

	sessionID, err := s.scanSvc.ScanLibraryAsync(r.Context(), *lib, "", false, poolSize)
	if err != nil {
		writeError(w, 500, "启动扫描失败: "+err.Error())
		return
	}
	writeJSON(w, 200, map[string]string{"session_id": sessionID, "status": "running"})
}

func (s *Server) handleRepairLibrary(w http.ResponseWriter, r *http.Request) {
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

	poolSize := 4
	if s.cfg != nil && s.cfg.Worker.PoolSize > 0 {
		poolSize = s.cfg.Worker.PoolSize
	}

	sessionID, err := s.scanSvc.RepairLibraryAsync(r.Context(), *lib, "", false, poolSize)
	if err != nil {
		writeError(w, 500, "启动修复扫描失败: "+err.Error())
		return
	}
	writeJSON(w, 200, map[string]string{"session_id": sessionID, "status": "running"})
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

	// 为临时扫描创建一个临时库记录（满足外键约束）
	tempLib := &repo.Library{
		Name: fmt.Sprintf("临时扫描-%s", filepath.Base(req.Path)),
		Path: req.Path,
		Kind: "mixed",
	}
	if err := s.libraries.Create(tempLib); err != nil {
		writeError(w, 500, "创建临时库失败: "+err.Error())
		return
	}

	poolSize := 4
	if s.cfg != nil && s.cfg.Worker.PoolSize > 0 {
		poolSize = s.cfg.Worker.PoolSize
	}

	sessionID, err := s.scanSvc.ScanLibraryAsync(r.Context(), *tempLib, "", true, poolSize)
	if err != nil {
		writeError(w, 500, "启动临时扫描失败: "+err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{
		"session_id": sessionID,
		"library_id": tempLib.ID,
		"status":     "running",
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

		// 迁移缩略图（从 _tmp 目录到正式目录）
		if m.ThumbnailPath != nil {
			srcThumb := filepath.Join(s.thumbBase, "_tmp", *m.ThumbnailPath)
			dstThumb := filepath.Join(s.thumbBase, *m.ThumbnailPath)
			if err := moveFile(srcThumb, dstThumb); err != nil {
				slog.Warn("迁移缩略图失败", "src", srcThumb, "err", err)
			}
		}

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

	det := duplicate.NewDetector(s.media, s.cfg)
	groups, err := det.DetectImageDuplicates()
	if err != nil {
		writeError(w, 500, "图片重复检测失败: "+err.Error())
		return
	}

	libs, _ := s.libraries.List()
	if err := duplicate.GenerateHTMLReport(groups, libs, "image", s.thumbBase, req.OutputPath); err != nil {
		writeError(w, 500, "生成报告失败: "+err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{
		"report_path": req.OutputPath,
		"groups":      len(groups),
	})
}

func (s *Server) handleVideoReport(w http.ResponseWriter, r *http.Request) {
	var req reportReq
	if err := parseJSON(r, &req); err != nil || req.OutputPath == "" {
		req.OutputPath = "report_video.html"
	}

	det := duplicate.NewDetector(s.media, s.cfg)
	groups, err := det.DetectVideoDuplicates()
	if err != nil {
		writeError(w, 500, "视频重复检测失败: "+err.Error())
		return
	}

	libs, _ := s.libraries.List()
	if err := duplicate.GenerateHTMLReport(groups, libs, "video", s.thumbBase, req.OutputPath); err != nil {
		writeError(w, 500, "生成报告失败: "+err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{
		"report_path": req.OutputPath,
		"groups":      len(groups),
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
