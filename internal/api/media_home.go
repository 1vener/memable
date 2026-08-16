// media_home.go：首页媒体列表、目录分组和统计接口。
// 代码注释使用中文。
package api

import (
	"net/http"
	"strconv"
)

const (
	defaultMediaPageSize   = 20
	maxMediaPageSize       = 100
	defaultMediaGroupLimit = 20
	maxMediaGroupLimit     = 100
)

// handleListMedia 返回正式媒体分页。
func (s *Server) handleListMedia(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	if kind != "image" && kind != "video" {
		writeError(w, http.StatusBadRequest, "kind 必须是 image 或 video")
		return
	}
	page, err := queryInt(r, "page", 1)
	if err != nil {
		writeError(w, http.StatusBadRequest, "page 必须是整数")
		return
	}
	pageSize, err := queryInt(r, "page_size", defaultMediaPageSize)
	if err != nil {
		writeError(w, http.StatusBadRequest, "page_size 必须是整数")
		return
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 1
	}
	if pageSize > maxMediaPageSize {
		pageSize = maxMediaPageSize
	}
	result, err := s.media.ListFormalPage(kind, page, pageSize)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// handleListMediaGroups 返回跨正式收藏库的目录分组，offset/limit 只作用于分组。
func (s *Server) handleListMediaGroups(w http.ResponseWriter, r *http.Request) {
	depth, err := queryInt(r, "depth", 1)
	if err != nil {
		writeError(w, http.StatusBadRequest, "depth 必须是整数")
		return
	}
	offset, err := queryInt(r, "offset", 0)
	if err != nil {
		writeError(w, http.StatusBadRequest, "offset 必须是整数")
		return
	}
	limit, err := queryInt(r, "limit", defaultMediaGroupLimit)
	if err != nil {
		writeError(w, http.StatusBadRequest, "limit 必须是整数")
		return
	}
	if depth < 1 {
		depth = 1
	}
	if offset < 0 {
		offset = 0
	}
	if limit < 1 {
		limit = 1
	}
	if limit > maxMediaGroupLimit {
		limit = maxMediaGroupLimit
	}
	groups, total, err := s.media.ListFormalGroups(depth, offset, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"total":  total,
		"offset": offset,
		"limit":  limit,
		"items":  groups,
	})
}

// handleMediaStatistics 返回正式媒体聚合统计。
func (s *Server) handleMediaStatistics(w http.ResponseWriter, r *http.Request) {
	stats, err := s.media.FormalStatistics()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func queryInt(r *http.Request, name string, def int) (int, error) {
	s := r.URL.Query().Get(name)
	if s == "" {
		return def, nil
	}
	return strconv.Atoi(s)
}
