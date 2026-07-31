// service.go：重复报告服务（生成、查询、清除、删除），负责三张表的读写与本地文件删除。
// 代码注释使用中文。
package duplicate

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"memable/internal/config"
	"memable/internal/recycle"
	"memable/internal/repo"
)

// Service 重复报告服务。
type Service struct {
	Dup            *repo.DuplicateRepo
	Media          *repo.MediaRepo
	Libraries      *repo.LibraryRepo
	Config         *config.Config
	ImageThumbBase string // 图片缩略图根目录
	VideoThumbBase string // 视频封面根目录
}

// NewService 创建重复报告服务。
func NewService(dup *repo.DuplicateRepo, mr *repo.MediaRepo, lr *repo.LibraryRepo, cfg *config.Config, imageThumbBase, videoThumbBase string) *Service {
	return &Service{Dup: dup, Media: mr, Libraries: lr, Config: cfg, ImageThumbBase: imageThumbBase, VideoThumbBase: videoThumbBase}
}

// Generate 检测并持久化重复报告（单事务替换旧报告），返回报告记录。
func (s *Service) Generate(opts Options, taskID string) (*repo.DuplicateReport, error) {
	det := NewDetector(s.Media, s.Config)
	groups, err := det.DetectWithOptions(opts)
	if err != nil {
		return nil, err
	}
	persist := make([]repo.PersistGroup, 0, len(groups))
	for _, g := range groups {
		pg := repo.PersistGroup{GroupType: reasonToGroupType(g.Reason)}
		for _, m := range g.Media {
			pg.MediaIDs = append(pg.MediaIDs, m.ID)
		}
		persist = append(persist, pg)
	}
	rep := &repo.DuplicateReport{
		Scope:               opts.Scope,
		MediaType:           opts.MediaType,
		ImageThreshold:      opts.ImageThreshold,
		VideoPhashDistance:  opts.VideoPhashDistance,
		VideoDurationDiffMs: opts.VideoDurationDiffMs,
		OshashFilter:        opts.OshashFilter,
		IncludeSHA1:         opts.IncludeSHA1,
	}
	if taskID != "" {
		rep.BackgroundTaskID = &taskID
	}
	id, err := s.Dup.ReplaceReport(rep, persist)
	if err != nil {
		return nil, err
	}
	rep.ID = id
	if stored, err := s.Dup.GetReportByID(id); err == nil && stored != nil {
		rep = stored
	}
	return rep, nil
}

func reasonToGroupType(reason string) string {
	switch reason {
	case "sha1_exact":
		return "sha1"
	case "phash_similar":
		return "image_similar"
	case "sprite_phash_similar":
		return "video_similar"
	default:
		return "sha1"
	}
}

// ReportSummary 报告摘要。
type ReportSummary struct {
	Report     *repo.DuplicateReport
	FreedBytes int64 // 可释放空间（每组保留 1 个）
}

// Summary 返回最新报告摘要。
func (s *Service) Summary() (*ReportSummary, error) {
	rep, err := s.Dup.GetLatestReport()
	if err != nil || rep == nil {
		return nil, err
	}
	views, err := s.Dup.GroupViews(rep.ID)
	if err != nil {
		return nil, err
	}
	freed := int64(0)
	for _, v := range views {
		freed += groupFreedBytes(v.Items)
	}
	return &ReportSummary{Report: rep, FreedBytes: freed}, nil
}

// GroupItem 分组展示项。
type GroupItem struct {
	ID          int64            `json:"id"`
	GroupType   string           `json:"group_type"`
	Directory   string           `json:"directory,omitempty"`
	MemberCount int              `json:"member_count"`
	FreedBytes  int64            `json:"freed_bytes"`
	Items       []repo.MediaView `json:"items"`
}

// GroupPage 分组分页结果。
type GroupPage struct {
	Total      int         `json:"total"`
	Page       int         `json:"page"`
	PageSize   int         `json:"page_size"`
	TotalPages int         `json:"total_pages"`
	Items      []GroupItem `json:"items"`
}

// Groups 返回最新报告的分组分页数据；kind 为 all/image/video 过滤，
// directory 非空时只返回包含该目录成员的分组（用于目录树局部刷新）。
func (s *Service) Groups(page, pageSize int, kind, directory string) (*GroupPage, error) {
	rep, err := s.Dup.GetLatestReport()
	if err != nil {
		return nil, err
	}
	if rep == nil {
		return &GroupPage{Page: page, PageSize: pageSize, Items: []GroupItem{}}, nil
	}
	views, err := s.Dup.GroupViews(rep.ID)
	if err != nil {
		return nil, err
	}
	items := make([]GroupItem, 0, len(views))
	for _, v := range views {
		if len(v.Items) == 0 {
			continue
		}
		if kind != "" && kind != "all" {
			memberKind := v.Items[0].Kind
			if memberKind != kind {
				continue
			}
		}
		if directory != "" {
			hasMember := false
			for _, m := range v.Items {
				if relDir(m.RelativePath) == directory {
					hasMember = true
					break
				}
			}
			if !hasMember {
				continue
			}
		}
		items = append(items, GroupItem{
			ID:          v.ID,
			GroupType:   v.GroupType,
			Directory:   relDir(v.Items[0].RelativePath),
			MemberCount: len(v.Items),
			FreedBytes:  groupFreedBytes(v.Items),
			Items:       v.Items,
		})
	}
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	total := len(items)
	totalPages := (total + pageSize - 1) / pageSize
	if totalPages < 1 {
		totalPages = 1
	}
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return &GroupPage{
		Total:      total,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
		Items:      items[start:end],
	}, nil
}

// TreeItem 报告目录树节点（相对库根目录）。
type TreeItem struct {
	Name      string      `json:"name"`
	Path      string      `json:"path"`
	FileCount int         `json:"file_count"`
	Children  []*TreeItem `json:"children,omitempty"`
}

// Tree 返回最新报告中包含重复文件的目录树。
func (s *Service) Tree() ([]*TreeItem, error) {
	rep, err := s.Dup.GetLatestReport()
	if err != nil {
		return nil, err
	}
	if rep == nil {
		return []*TreeItem{}, nil
	}
	views, err := s.Dup.GroupViews(rep.ID)
	if err != nil {
		return nil, err
	}
	dirs := map[string]int{}
	for _, v := range views {
		for _, m := range v.Items {
			d := relDir(m.RelativePath)
			dirs[d]++
		}
	}
	return buildDirTree(dirs), nil
}

// buildDirTree 将"目录 → 直属重复文件数"构建为嵌套目录树。
// 实现：先为每个路径段创建节点，再按父路径挂接，避免递归重建导致的栈溢出。
func buildDirTree(dirs map[string]int) []*TreeItem {
	nodes := map[string]*TreeItem{}
	for dir := range dirs {
		if dir == "." || dir == "" {
			continue
		}
		parts := strings.Split(dir, "/")
		prefix := ""
		for i, p := range parts {
			if i > 0 {
				prefix += "/"
			}
			prefix += p
			if _, ok := nodes[prefix]; !ok {
				nodes[prefix] = &TreeItem{Name: p, Path: prefix}
			}
		}
	}
	// 挂接父子关系与直属文件数
	for dir, cnt := range dirs {
		if dir == "." || dir == "" {
			continue
		}
		node := nodes[dir]
		node.FileCount += cnt
		parent := parentDir(dir)
		if p, ok := nodes[parent]; ok {
			p.Children = append(p.Children, node)
		}
	}
	// 根节点 = 父路径不在节点表内的节点（含只有子目录、无直属文件的中间层）
	roots := make([]*TreeItem, 0, len(nodes))
	for path, node := range nodes {
		parent := parentDir(path)
		if parent == "" {
			roots = append(roots, node)
		} else if _, ok := nodes[parent]; !ok {
			roots = append(roots, node)
		}
	}
	for _, n := range nodes {
		sort.Slice(n.Children, func(i, j int) bool {
			return n.Children[i].Name < n.Children[j].Name
		})
	}
	// 根目录直属文件（目录为 "." 或 ""）作为根节点返回
	if cnt := dirs["."] + dirs[""]; cnt > 0 {
		root := &TreeItem{Name: ".", Path: "", FileCount: cnt}
		roots = append([]*TreeItem{root}, roots...)
	}
	return roots
}

// parentDir 返回路径的父目录（正斜杠）；无父目录时返回空串。
func parentDir(dir string) string {
	idx := strings.LastIndex(dir, "/")
	if idx < 0 {
		return ""
	}
	return dir[:idx]
}

// ClearRequest 一键清除请求。
type ClearRequest struct {
	Scope     string  `json:"scope"`               // directory / page / group
	Keep      string  `json:"keep"`                // largest/smallest/newest/oldest/longest_name/shortest_name
	Directory string  `json:"directory,omitempty"` // scope=directory 时的相对目录
	GroupIDs  []int64 `json:"group_ids,omitempty"` // scope=page 时的组 ID 列表
	GroupID   int64   `json:"group_id,omitempty"`  // scope=group 时的组 ID
	Permanent bool    `json:"permanent,omitempty"`
}

// ClearResult 一键清除结果。
type ClearResult struct {
	DeletedFiles    int   `json:"deleted_files"`
	FreedBytes      int64 `json:"freed_bytes"`
	RemainingGroups int   `json:"remaining_groups"`
}

// Clear 一键清除重复文件：每组按保留条件保留 1 个，其余删除（含缩略图/media 记录/本地文件）。
func (s *Service) Clear(req ClearRequest) (*ClearResult, error) {
	rep, err := s.Dup.GetLatestReport()
	if err != nil {
		return nil, err
	}
	if rep == nil {
		return nil, fmt.Errorf("尚无重复报告")
	}
	views, err := s.Dup.GroupViews(rep.ID)
	if err != nil {
		return nil, err
	}
	targets := make([]repo.GroupView, 0, len(views))
	switch req.Scope {
	case "directory":
		for _, v := range views {
			if len(v.Items) == 0 {
				continue
			}
			d := relDir(v.Items[0].RelativePath)
			allSame := true
			for _, m := range v.Items {
				if relDir(m.RelativePath) != d {
					allSame = false
					break
				}
			}
			if allSame && d == req.Directory {
				targets = append(targets, v)
			}
		}
	case "page":
		ids := map[int64]bool{}
		for _, id := range req.GroupIDs {
			ids[id] = true
		}
		for _, v := range views {
			if ids[v.ID] {
				targets = append(targets, v)
			}
		}
	case "group":
		for _, v := range views {
			if v.ID == req.GroupID {
				targets = append(targets, v)
				break
			}
		}
	default:
		return nil, fmt.Errorf("无效的清除范围: %s", req.Scope)
	}

	toDelete := make([]int64, 0)
	for _, v := range targets {
		keepIdx := keepIndex(v.Items, req.Keep)
		for i, m := range v.Items {
			if i != keepIdx {
				toDelete = append(toDelete, m.ID)
			}
		}
	}
	if len(toDelete) == 0 {
		remaining := 0
		if latest, _ := s.Dup.GetLatestReport(); latest != nil {
			remaining = latest.TotalGroups
		}
		return &ClearResult{RemainingGroups: remaining}, nil
	}
	res, err := s.DeleteMedia(toDelete, req.Permanent)
	if err != nil {
		return nil, err
	}
	remaining := 0
	if latest, _ := s.Dup.GetLatestReport(); latest != nil {
		remaining = latest.TotalGroups
	}
	return &ClearResult{DeletedFiles: res.DeletedFiles, FreedBytes: res.FreedBytes, RemainingGroups: remaining}, nil
}

// DeleteResult 删除结果。
type DeleteResult struct {
	DeletedFiles int   `json:"deleted_files"`
	FreedBytes   int64 `json:"freed_bytes"`
}

// DeleteMedia 删除媒体：源文件默认移入回收站（permanent=true 时永久删除），
// 同时删除缩略图文件与 media 记录（重复组成员随外键级联），并刷新重复报告统计。
func (s *Service) DeleteMedia(ids []int64, permanent bool) (*DeleteResult, error) {
	if len(ids) == 0 {
		return &DeleteResult{}, nil
	}
	freed := int64(0)
	thumbRefs := make([]repo.ThumbRef, 0)
	deleted := 0
	for _, id := range ids {
		m, err := s.Media.GetByID(id)
		if err != nil {
			return nil, err
		}
		if m == nil {
			continue
		}
		lib, err := s.Libraries.GetByID(m.LibraryID)
		if err != nil {
			return nil, err
		}
		full, err := safeFullPath(lib.Path, m.RelativePath)
		if err != nil {
			return nil, err
		}
		// 删除源文件：默认回收站，失败则中止（避免数据库与磁盘不一致）
		if permanent {
			if err := os.Remove(full); err != nil && !os.IsNotExist(err) {
				return nil, fmt.Errorf("永久删除失败 %s: %w", full, err)
			}
		} else {
			if err := recycle.ToBin(full); err != nil {
				return nil, fmt.Errorf("移入回收站失败 %s: %w", full, err)
			}
		}
		if m.ThumbnailPath != nil && *m.ThumbnailPath != "" {
			thumbRefs = append(thumbRefs, repo.ThumbRef{Kind: m.Kind, Rel: *m.ThumbnailPath})
		}
		freed += m.FileSize
		deleted++
	}
	if deleted == 0 {
		return &DeleteResult{}, nil
	}
	// 删除 media 记录（重复组成员外键级联）
	if _, err := s.Media.DeleteByIDs(ids); err != nil {
		return nil, err
	}
	// 清理无引用缩略图
	for _, ref := range thumbRefs {
		n, err := s.Media.CountThumbnailReferences(ref.Rel)
		if err != nil || n > 0 {
			continue
		}
		abs := filepath.Join(s.thumbBaseFor(ref.Kind), filepath.FromSlash(ref.Rel))
		if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
			// 缩略图清理失败不影响删除结果
		}
	}
	// 刷新重复报告（清理 <2 的组、更新统计）
	if err := s.Dup.PruneGroupsAndUpdateStats(); err != nil {
		return nil, err
	}
	return &DeleteResult{DeletedFiles: deleted, FreedBytes: freed}, nil
}

// thumbBaseFor 按媒体类型返回缩略图根目录（显式字段优先，其次配置/系统默认）。
func (s *Service) thumbBaseFor(kind string) string {
	if kind == "video" {
		if s.VideoThumbBase != "" {
			return s.VideoThumbBase
		}
		if s.Config != nil {
			return s.Config.VideoThumbDir()
		}
		return "thumbnail"
	}
	if s.ImageThumbBase != "" {
		return s.ImageThumbBase
	}
	if s.Config != nil {
		return s.Config.ImageThumbDir()
	}
	return "thumbnail"
}

// SetStale 标记最新报告需要重新生成（新增/重新处理导致数据变化时调用）。
func (s *Service) SetStale() error {
	return s.Dup.SetStaleOnLatest()
}

// PruneAfterMediaChange 删除类变更后清理重复组并刷新统计。
func (s *Service) PruneAfterMediaChange() error {
	return s.Dup.PruneGroupsAndUpdateStats()
}

// safeFullPath 校验完整路径位于收藏库根目录内。
func safeFullPath(libPath, relPath string) (string, error) {
	rootAbs, err := filepath.Abs(filepath.Clean(libPath))
	if err != nil {
		return "", err
	}
	fullAbs, err := filepath.Abs(filepath.Join(libPath, filepath.FromSlash(relPath)))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(rootAbs, fullAbs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("路径越界: %s", fullAbs)
	}
	return fullAbs, nil
}

// groupFreedBytes 每组可释放空间 = 总大小 - 最大文件（假设保留最大文件）。
func groupFreedBytes(items []repo.MediaView) int64 {
	if len(items) < 2 {
		return 0
	}
	var sum, max int64
	for _, m := range items {
		sum += m.FileSize
		if m.FileSize > max {
			max = m.FileSize
		}
	}
	return sum - max
}

// keepIndex 按保留条件选出应保留的文件下标。
func keepIndex(items []repo.MediaView, keep string) int {
	if len(items) == 0 {
		return 0
	}
	best := 0
	for i := 1; i < len(items); i++ {
		if betterKeep(items[i], items[best], keep) {
			best = i
		}
	}
	return best
}

func betterKeep(a, b repo.MediaView, keep string) bool {
	switch keep {
	case "largest":
		return a.FileSize > b.FileSize
	case "smallest":
		return a.FileSize < b.FileSize
	case "newest":
		return a.Mtime.After(b.Mtime)
	case "oldest":
		return a.Mtime.Before(b.Mtime)
	case "longest_name":
		return len(filepath.Base(filepath.FromSlash(a.RelativePath))) > len(filepath.Base(filepath.FromSlash(b.RelativePath)))
	case "shortest_name":
		return len(filepath.Base(filepath.FromSlash(a.RelativePath))) < len(filepath.Base(filepath.FromSlash(b.RelativePath)))
	default:
		return false
	}
}
