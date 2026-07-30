// 包 api：HTTP REST API 服务。
// 代码注释使用中文。
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"memable/internal/config"
	"memable/internal/repo"
	"memable/internal/scan"
	"memable/internal/search"
	"memable/internal/task"
)

// Server HTTP API 服务器。
type Server struct {
	cfg       *config.Config
	libraries *repo.LibraryRepo
	sessions  *repo.SessionRepo
	media     *repo.MediaRepo
	tasks     *repo.TaskRepo
	fileStats *repo.FileStatsRepo
	scanSvc   *scan.Service
	searchSvc *search.Service
	runner    *task.Runner
	thumbBase string
	http      *http.Server
}

// NewServer 创建 HTTP API 服务器。
func NewServer(cfg *config.Config, lr *repo.LibraryRepo, sr *repo.SessionRepo, mr *repo.MediaRepo, tr *repo.TaskRepo, fsr *repo.FileStatsRepo, scanSvc *scan.Service, searchSvc *search.Service, runner *task.Runner, thumbBase string) *Server {
	s := &Server{
		cfg:       cfg,
		libraries: lr,
		sessions:  sr,
		media:     mr,
		tasks:     tr,
		fileStats: fsr,
		scanSvc:   scanSvc,
		searchSvc: searchSvc,
		runner:    runner,
		thumbBase: thumbBase,
	}
	mux := http.NewServeMux()
	s.registerRoutes(mux)
	s.http = &http.Server{
		Addr:         ":8080",
		Handler:      corsMiddleware(mux),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 120 * time.Second,
	}
	return s
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

	// 扫描
	mux.HandleFunc("POST /api/libraries/{id}/scan", s.handleScanLibrary)
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

	// 缩略图静态服务
	mux.HandleFunc("GET /api/thumbnails/", s.handleThumbnail)

	// 媒体操作
	mux.HandleFunc("POST /api/media/{id}/open", s.handleOpenMedia)

	// 健康检查
	mux.HandleFunc("GET /api/health", s.handleHealth)

	// 任务管理
	mux.HandleFunc("GET /api/tasks", s.handleListTasks)
	mux.HandleFunc("GET /api/tasks/{id}", s.handleGetTask)
	mux.HandleFunc("POST /api/tasks/{id}/cancel", s.handleCancelTask)

	// 工具 - 文件统计
	mux.HandleFunc("POST /api/tools/file-stats", s.handleCreateFileStats)
	mux.HandleFunc("GET /api/tools/file-stats", s.handleListFileStats)
	mux.HandleFunc("GET /api/tools/file-stats/{id}", s.handleGetFileStats)
	mux.HandleFunc("DELETE /api/tools/file-stats/{id}", s.handleDeleteFileStats)
}

// Start 启动 HTTP 服务器。
func (s *Server) Start() error {
	slog.Info("HTTP API 启动", "addr", s.http.Addr)
	return s.http.ListenAndServe()
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

// thumbAbsPath 返回缩略图的绝对路径。
func (s *Server) thumbAbsPath(relPath string) string {
	return filepath.Join(s.thumbBase, relPath)
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
