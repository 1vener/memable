// directory_compare.go：目录对比报告（所选目录 vs 存量数据）HTTP 处理器。
// 独立存储于 dir_duplicate_* 三表，不替换重复报告。
// 代码注释使用中文。
package api

import (
	"net/http"
	"strconv"
	"strings"

	"memable/internal/duplicate"
	"memable/internal/repo"
	"memable/internal/task"
)

// dirCompareReq 生成目录对比报告请求。
type dirCompareReq struct {
	LibraryID           int64  `json:"library_id"`
	Directory           string `json:"directory"` // 相对库根路径（正斜杠，含子目录）
	MediaType           string `json:"media_type"`
	ImageThreshold      *int   `json:"image_threshold,omitempty"`
	VideoPhashDistance  *int   `json:"video_phash_distance,omitempty"`
	VideoDurationDiffMs *int64 `json:"video_duration_diff_ms,omitempty"`
	OshashFilter        *bool  `json:"oshash_filter,omitempty"`
	IncludeSHA1         *bool  `json:"include_sha1,omitempty"`
}

// handleCreateDirCompare 提交目录对比报告生成任务（后台执行）。
func (s *Server) handleCreateDirCompare(w http.ResponseWriter, r *http.Request) {
	if s.dup == nil {
		writeError(w, 500, "重复报告服务未初始化")
		return
	}
	var req dirCompareReq
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
	req.Directory = strings.Trim(strings.ReplaceAll(req.Directory, "\\", "/"), "/")
	if req.Directory == "" {
		req.Directory = "." // 库根目录
	}
	if req.MediaType == "" {
		req.MediaType = "all"
	}
	if req.MediaType != "all" && req.MediaType != "image" && req.MediaType != "video" {
		writeError(w, 400, "media_type 必须是 all/image/video")
		return
	}
	if s.runner == nil {
		writeError(w, 500, "任务调度器未初始化")
		return
	}

	// 阈值默认值与重复报告一致
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
		MediaType:           req.MediaType,
		ImageThreshold:      imageThreshold,
		VideoPhashDistance:  videoDist,
		VideoDurationDiffMs: durDiff,
		OshashFilter:        oshashFilter,
		IncludeSHA1:         includeSHA1,
	}
	dedupeKey := "dir_compare:" + strconv.FormatInt(req.LibraryID, 10) + ":" + req.Directory
	taskID, err := s.runner.Enqueue(repo.TaskKindReportDirectory, "目录对比: "+req.Directory,
		&dedupeKey, &req.LibraryID, task.DirComparePayload{
			Options:   opts,
			LibraryID: req.LibraryID,
			Directory: req.Directory,
		})
	if err != nil {
		writeError(w, 409, "相同任务已在等待或执行")
		return
	}
	pos, _ := s.tasks.QueuePosition(taskID.ID)
	writeJSON(w, 202, map[string]any{
		"task_id":        taskID.ID,
		"status":         taskID.Status,
		"queue_position": pos,
	})
}

// handleGetDirCompare 返回最新目录对比报告摘要。
func (s *Server) handleGetDirCompare(w http.ResponseWriter, r *http.Request) {
	if s.dup == nil {
		writeError(w, 500, "重复报告服务未初始化")
		return
	}
	summary, err := s.dup.DirSummary()
	if err != nil {
		writeError(w, 500, "查询目录对比报告失败: "+err.Error())
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

// handleListDirCompareGroups 返回目录对比分组分页数据。
func (s *Server) handleListDirCompareGroups(w http.ResponseWriter, r *http.Request) {
	if s.dup == nil {
		writeError(w, 500, "重复报告服务未初始化")
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = s.cfg.UI.DefaultPageSize
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	pageData, err := s.dup.DirGroups(page, pageSize)
	if err != nil {
		writeError(w, 500, "查询目录对比分组失败: "+err.Error())
		return
	}
	writeJSON(w, 200, pageData)
}
