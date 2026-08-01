// report_duplicate.go：重复报告（三张表持久化）相关 HTTP 处理器。
// 代码注释使用中文。
package api

import (
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strconv"

	"memable/internal/duplicate"
	"memable/internal/repo"
)

// reportDuplicateReq 生成重复报告请求。
type reportDuplicateReq struct {
	Scope               string `json:"scope"`
	MediaType           string `json:"media_type"`
	ImageThreshold      *int   `json:"image_threshold,omitempty"`
	VideoPhashDistance  *int   `json:"video_phash_distance,omitempty"`
	VideoDurationDiffMs *int64 `json:"video_duration_diff_ms,omitempty"`
	OshashFilter        *bool  `json:"oshash_filter,omitempty"`
	IncludeSHA1         *bool  `json:"include_sha1,omitempty"`
}

// handleCreateDuplicateReport 提交重复报告生成任务（后台执行）。
func (s *Server) handleCreateDuplicateReport(w http.ResponseWriter, r *http.Request) {
	var req reportDuplicateReq
	if err := parseJSON(r, &req); err != nil {
		writeError(w, 400, "请求体格式错误")
		return
	}
	if s.dup == nil {
		writeError(w, 500, "重复报告服务未初始化")
		return
	}
	if req.Scope == "" {
		req.Scope = "all"
	}
	if req.Scope != "all" && req.Scope != "same_dir" {
		writeError(w, 400, "scope 必须是 all 或 same_dir")
		return
	}
	if req.MediaType == "" {
		req.MediaType = "all"
	}
	if req.MediaType != "all" && req.MediaType != "image" && req.MediaType != "video" {
		writeError(w, 400, "media_type 必须是 all/image/video")
		return
	}

	// 默认值：与旧版报告一致，图片阈值由配置的距离换算为百分比
	def := s.reportDefaults()
	imageThreshold := def["image_threshold"].(int)
	if req.ImageThreshold != nil {
		imageThreshold = *req.ImageThreshold
	}
	if imageThreshold < 0 {
		imageThreshold = 0
	}
	if imageThreshold > 100 {
		imageThreshold = 100
	}
	videoDist := def["video_phash_distance"].(int)
	if req.VideoPhashDistance != nil {
		videoDist = *req.VideoPhashDistance
	}
	durDiff := def["video_duration_diff_ms"].(int64)
	if req.VideoDurationDiffMs != nil {
		durDiff = *req.VideoDurationDiffMs
	}
	oshashFilter := def["oshash_filter"].(bool)
	if req.OshashFilter != nil {
		oshashFilter = *req.OshashFilter
	}
	includeSHA1 := def["include_sha1"].(bool)
	if req.IncludeSHA1 != nil {
		includeSHA1 = *req.IncludeSHA1
	}

	opts := duplicate.Options{
		Scope:               req.Scope,
		MediaType:           req.MediaType,
		ImageThreshold:      imageThreshold,
		VideoPhashDistance:  videoDist,
		VideoDurationDiffMs: durDiff,
		OshashFilter:        oshashFilter,
		IncludeSHA1:         includeSHA1,
	}
	dedupeKey := "report_duplicate"
	task, err := s.runner.Enqueue(repo.TaskKindReportDuplicate, "重复统计报告",
		&dedupeKey, nil, map[string]any{"options": opts})
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

// reportDefaults 返回报告生成选项的默认值（配置优先，与创建接口解析逻辑一致）。
func (s *Server) reportDefaults() map[string]any {
	imageThreshold := 90 // PRD 兜底
	if s.cfg.Similarity.ImagePHashDistance > 0 {
		imageThreshold = percentFromDistance(s.cfg.Similarity.ImagePHashDistance)
	}
	videoDist := s.cfg.Similarity.VideoPHashDistance
	if videoDist <= 0 {
		videoDist = 12
	}
	durDiff := s.cfg.Similarity.VideoDurationDiffMs
	if durDiff <= 0 {
		durDiff = 3000
	}
	return map[string]any{
		"image_threshold":        imageThreshold,
		"video_phash_distance":   videoDist,
		"video_duration_diff_ms": durDiff,
		"oshash_filter":          true,
		"include_sha1":           true,
	}
}

// percentFromDistance 将 pHash Hamming 距离上限换算为相似度百分比（0-100）。
func percentFromDistance(dist int) int {
	if dist <= 0 {
		return 90
	}
	if dist >= 64 {
		return 0
	}
	return int(math.Round(100 - float64(dist)*100/64))
}

// handleGetDuplicateReportDefaults 返回报告生成选项默认值，供客户端对话框使用。
func (s *Server) handleGetDuplicateReportDefaults(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.reportDefaults())
}

// handleGetDuplicateReport 返回最新报告摘要（含可释放空间与 stale 标记）。
func (s *Server) handleGetDuplicateReport(w http.ResponseWriter, r *http.Request) {
	if s.dup == nil {
		writeError(w, 500, "重复报告服务未初始化")
		return
	}
	summary, err := s.dup.Summary()
	if err != nil {
		writeError(w, 500, "查询重复报告失败: "+err.Error())
		return
	}
	if summary == nil || summary.Report == nil {
		writeJSON(w, 200, map[string]any{"report": nil, "freed_bytes": 0})
		return
	}
	writeJSON(w, 200, map[string]any{
		"report":      summary.Report,
		"freed_bytes": summary.FreedBytes,
	})
}

// handleListDuplicateGroups 返回分组分页数据。
func (s *Server) handleListDuplicateGroups(w http.ResponseWriter, r *http.Request) {
	if s.dup == nil {
		writeError(w, 500, "重复报告服务未初始化")
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	kind := r.URL.Query().Get("kind")
	directory := r.URL.Query().Get("directory")
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = s.cfg.UI.DefaultPageSize
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	pageData, err := s.dup.Groups(page, pageSize, kind, directory)
	if err != nil {
		writeError(w, 500, "查询重复分组失败: "+err.Error())
		return
	}
	writeJSON(w, 200, pageData)
}

// handleDuplicateReportTree 返回报告中包含重复文件的目录树。
func (s *Server) handleDuplicateReportTree(w http.ResponseWriter, r *http.Request) {
	if s.dup == nil {
		writeError(w, 500, "重复报告服务未初始化")
		return
	}
	tree, err := s.dup.Tree()
	if err != nil {
		writeError(w, 500, "查询重复报告目录树失败: "+err.Error())
		return
	}
	writeJSON(w, 200, tree)
}

// handleExcludeDuplicateMedia 从当前重复报告中排除指定媒体（人工筛选无重复）。
// 仅对当前报告生效：重新生成报告后该文件重新参与检测。
func (s *Server) handleExcludeDuplicateMedia(w http.ResponseWriter, r *http.Request) {
	if s.dup == nil {
		writeError(w, 500, "重复报告服务未初始化")
		return
	}
	var req struct {
		MediaID int64 `json:"media_id"`
	}
	if err := parseJSON(r, &req); err != nil {
		writeError(w, 400, "请求体格式错误")
		return
	}
	if req.MediaID <= 0 {
		writeError(w, 400, "media_id 必须为正整数")
		return
	}
	removed, err := s.dup.ExcludeMedia(req.MediaID)
	if err != nil {
		writeError(w, 500, "排除重复失败: "+err.Error())
		return
	}
	slog.Info("排除重复完成", "media_id", req.MediaID, "removed_members", removed)
	writeJSON(w, 200, map[string]any{"status": "ok", "removed_members": removed})
}

// handleClearDuplicateReport 一键清除重复文件（按目录/整页/单组 + 保留条件）。
func (s *Server) handleClearDuplicateReport(w http.ResponseWriter, r *http.Request) {
	if s.dup == nil {
		writeError(w, 500, "重复报告服务未初始化")
		return
	}
	var req duplicate.ClearRequest
	if err := parseJSON(r, &req); err != nil {
		writeError(w, 400, "请求体格式错误")
		return
	}
	if req.Scope != "directory" && req.Scope != "page" && req.Scope != "group" {
		writeError(w, 400, "scope 必须是 directory/page/group")
		return
	}
	if req.Keep == "" {
		req.Keep = "largest"
	}
	permanent := s.cfg.Delete.Permanent
	req.Permanent = permanent
	result, err := s.dup.Clear(req)
	if err != nil {
		writeError(w, 500, "清除重复文件失败: "+err.Error())
		return
	}
	slog.Info("清除重复文件完成", "deleted", result.DeletedFiles, "freed", result.FreedBytes)
	writeJSON(w, 200, result)
}

// handleDeleteMedia 删除媒体（源文件默认进回收站，可永久删除）。
func (s *Server) handleDeleteMedia(w http.ResponseWriter, r *http.Request) {
	if s.dup == nil {
		writeError(w, 500, "重复报告服务未初始化")
		return
	}
	var req struct {
		MediaIDs  []int64 `json:"media_ids"`
		Permanent *bool   `json:"permanent,omitempty"`
	}
	if err := parseJSON(r, &req); err != nil {
		writeError(w, 400, "请求体格式错误")
		return
	}
	if len(req.MediaIDs) == 0 {
		writeError(w, 400, "media_ids 不能为空")
		return
	}
	permanent := s.cfg.Delete.Permanent
	if req.Permanent != nil {
		permanent = *req.Permanent
	}
	result, err := s.dup.DeleteMedia(req.MediaIDs, permanent)
	if err != nil {
		writeError(w, 500, fmt.Sprintf("删除媒体失败: %v", err))
		return
	}
	writeJSON(w, 200, result)
}
