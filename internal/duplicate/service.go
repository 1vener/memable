// service.go：重复报告服务（生成、查询、清除、删除），负责三张表的读写与本地文件删除。
// 代码注释使用中文。
package duplicate

import (
	"fmt"
	"log/slog"
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
	Dir            *repo.DirDuplicateRepo // 目录对比报告（独立存储）
	Media          *repo.MediaRepo
	Libraries      *repo.LibraryRepo
	Config         *config.Config
	ImageThumbBase string // 图片缩略图根目录
	VideoThumbBase string // 视频封面根目录
	// Progress 可选进度回调，报告生成时透传给检测器。
	Progress repo.ProgressFunc
}

// NewService 创建重复报告服务。
func NewService(dup *repo.DuplicateRepo, dir *repo.DirDuplicateRepo, mr *repo.MediaRepo, lr *repo.LibraryRepo, cfg *config.Config, imageThumbBase, videoThumbBase string) *Service {
	return &Service{Dup: dup, Dir: dir, Media: mr, Libraries: lr, Config: cfg, ImageThumbBase: imageThumbBase, VideoThumbBase: videoThumbBase}
}

// Generate 检测并持久化重复报告（单事务替换旧报告），返回报告记录。
// 当检测阈值/范围未变化时启用增量检测：仅对 created_at >= 上次报告时间
// 的新增/修改媒体发起查询，并与旧报告分组合并，大幅减少重复检测耗时。
func (s *Service) Generate(opts Options, taskID string) (*repo.DuplicateReport, error) {
	det := NewDetector(s.Media, s.Config)
	det.Progress = s.Progress

	// 增量检测判定：与上次报告参数一致且旧报告有分组时才增量。
	// 旧报告为空（如历史 bug 产生的空报告）时必须全量检测，避免空报告传染。
	var oldGroups []repo.PersistGroup
	incremental := false
	if last, err := s.Dup.GetLatestReport(); err == nil && last != nil && reportParamsEqual(last, opts) {
		// 读取旧报告分组，增量结果需与旧组合并（旧媒体之间的重复关系不在本次检测范围内）
		if views, err := s.Dup.GroupViews(last.ID); err == nil && len(views) > 0 {
			since := last.CreatedAt
			det.IncrementalSince = &since
			incremental = true
			slog.Info("重复报告增量检测", "since", since.UTC().Format("2006-01-02 15:04:05"))
			for _, v := range views {
				if len(v.Items) < 2 {
					continue
				}
				pg := repo.PersistGroup{GroupType: v.GroupType}
				for _, m := range v.Items {
					pg.MediaIDs = append(pg.MediaIDs, m.ID)
				}
				oldGroups = append(oldGroups, pg)
			}
		} else if err != nil {
			slog.Warn("读取旧报告分组失败，回退全量检测", "err", err)
		}
	}

	groups, err := det.DetectWithOptions(opts)
	if err != nil {
		return nil, err
	}
	persist := make([]repo.PersistGroup, 0, len(groups))
	if incremental && len(oldGroups) > 0 {
		// 增量：合并旧报告分组与新检测结果，避免旧媒体之间的重复组丢失
		persist = mergeWithOldGroups(groups, oldGroups)
	} else {
		for _, g := range groups {
			pg := repo.PersistGroup{GroupType: reasonToGroupType(g.Reason)}
			for _, m := range g.Media {
				pg.MediaIDs = append(pg.MediaIDs, m.ID)
			}
			persist = append(persist, pg)
		}
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

// mergeWithOldGroups 用并查集合并增量检测结果与旧报告分组。
// 旧组的重复关系保留；新检测产生的组（可能含旧媒体）与其重叠组自然合并。
func mergeWithOldGroups(newGroups []Group, oldGroups []repo.PersistGroup) []repo.PersistGroup {
	idIndex := map[int64]int{}
	var ids []int64
	add := func(id int64) int {
		if i, ok := idIndex[id]; ok {
			return i
		}
		idIndex[id] = len(ids)
		ids = append(ids, id)
		return len(ids) - 1
	}
	for _, g := range oldGroups {
		for _, id := range g.MediaIDs {
			add(id)
		}
	}
	for _, g := range newGroups {
		for _, m := range g.Media {
			add(m.ID)
		}
	}

	uf := newUnionFind(len(ids))
	// 旧组 union，记录分量类型（sha1 优先）
	oldType := map[int]string{}
	for _, g := range oldGroups {
		if len(g.MediaIDs) < 2 {
			continue
		}
		first := add(g.MediaIDs[0])
		for _, id := range g.MediaIDs[1:] {
			uf.union(first, add(id))
		}
		root := uf.find(first)
		if oldType[root] != "sha1" {
			oldType[root] = g.GroupType
		}
	}
	// 新组 union，记录新检测 reason（sha1 优先）
	newReason := map[int]string{}
	for _, g := range newGroups {
		if len(g.Media) < 2 {
			continue
		}
		first := add(g.Media[0].ID)
		for _, m := range g.Media[1:] {
			uf.union(first, add(m.ID))
		}
		root := uf.find(first)
		if newReason[root] != "sha1_exact" {
			newReason[root] = g.Reason
		}
	}

	// 输出连通分量
	comps := map[int][]int{}
	for i := range ids {
		root := uf.find(i)
		comps[root] = append(comps[root], i)
	}
	out := make([]repo.PersistGroup, 0, len(comps))
	for root, indices := range comps {
		if len(indices) < 2 {
			continue
		}
		gtype := oldType[root]
		if reason, ok := newReason[root]; ok {
			gtype = reasonToGroupType(reason)
		}
		if gtype == "" {
			gtype = "sha1"
		}
		pg := repo.PersistGroup{GroupType: gtype}
		for _, i := range indices {
			pg.MediaIDs = append(pg.MediaIDs, ids[i])
		}
		out = append(out, pg)
	}
	return out
}

// reportParamsEqual 判断本次报告参数与上次报告是否一致。
func reportParamsEqual(last *repo.DuplicateReport, opts Options) bool {
	return last.Scope == opts.Scope &&
		last.MediaType == opts.MediaType &&
		last.ImageThreshold == opts.ImageThreshold &&
		last.VideoPhashDistance == opts.VideoPhashDistance &&
		last.VideoDurationDiffMs == opts.VideoDurationDiffMs
}

// GenerateDirCompare 生成目录对比报告：所选目录（含子目录）与其余存量数据的重复检测，
// 写入独立三表（不替换重复报告）。目录对比始终全量检测（不做增量）。
func (s *Service) GenerateDirCompare(opts Options, libraryID int64, directory string, taskID string) (*repo.DirDuplicateReport, error) {
	det := NewDetector(s.Media, s.Config)
	det.Progress = s.Progress

	opts.Scope = "dir_vs_rest"
	opts.LibraryID = libraryID
	opts.Directory = directory

	slog.Info("目录对比检测开始", "library_id", libraryID, "directory", directory, "media_type", opts.MediaType)

	groups, err := det.DetectWithOptions(opts)
	if err != nil {
		return nil, err
	}

	persist := make([]repo.DirPersistGroup, 0, len(groups))
	for _, g := range groups {
		pg := repo.DirPersistGroup{GroupType: reasonToGroupType(g.Reason)}
		for _, m := range g.Media {
			pg.Members = append(pg.Members, repo.DirMemberPersist{
				MediaID:  m.ID,
				IsTarget: m.LibraryID == libraryID && isUnderDir(m.RelativePath, directory),
			})
		}
		persist = append(persist, pg)
	}

	rep := &repo.DirDuplicateReport{
		LibraryID:           libraryID,
		Directory:           directory,
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
	id, err := s.Dir.ReplaceDirReport(rep, persist)
	if err != nil {
		return nil, err
	}
	rep.ID = id
	// 重新读取统计快照（ReplaceDirReport 内 UPDATE 了 total_groups/total_files）
	if stored, err := s.Dir.GetLatestDirReport(); err == nil && stored != nil {
		rep = stored
	}
	return rep, nil
}

// DirReportSummary 目录对比报告摘要。
type DirReportSummary struct {
	Report     *repo.DirDuplicateReport
	FreedBytes int64 // 可释放空间（每组保留 1 个）
}

// DirSummary 返回最新目录对比报告摘要。
func (s *Service) DirSummary() (*DirReportSummary, error) {
	if s.Dir == nil {
		return nil, nil
	}
	rep, err := s.Dir.GetLatestDirReport()
	if err != nil || rep == nil {
		return nil, err
	}
	views, err := s.Dir.DirGroupViews(rep.ID)
	if err != nil {
		return nil, err
	}
	freed := int64(0)
	for _, v := range views {
		items := make([]repo.MediaView, 0, len(v.Items))
		for _, it := range v.Items {
			items = append(items, it.MediaView)
		}
		freed += groupFreedBytes(items)
	}
	return &DirReportSummary{Report: rep, FreedBytes: freed}, nil
}

// DirGroupItem 目录对比分组展示项。
type DirGroupItem struct {
	ID          int64                `json:"id"`
	GroupType   string               `json:"group_type"`
	MemberCount int                  `json:"member_count"`
	FreedBytes  int64                `json:"freed_bytes"`
	Items       []repo.DirMemberView `json:"items"`
}

// DirGroupPage 目录对比分组分页结果。
type DirGroupPage struct {
	Total      int            `json:"total"`
	Page       int            `json:"page"`
	PageSize   int            `json:"page_size"`
	TotalPages int            `json:"total_pages"`
	Items      []DirGroupItem `json:"items"`
}

// DirGroups 返回最新目录对比报告的分组分页数据；kind 非空时按媒体类型过滤组。
func (s *Service) DirGroups(page, pageSize int, kind string) (*DirGroupPage, error) {
	if s.Dir == nil {
		return &DirGroupPage{Page: page, PageSize: pageSize, Items: []DirGroupItem{}}, nil
	}
	rep, err := s.Dir.GetLatestDirReport()
	if err != nil {
		return nil, err
	}
	if rep == nil {
		return &DirGroupPage{Page: page, PageSize: pageSize, Items: []DirGroupItem{}}, nil
	}
	views, err := s.Dir.DirGroupViews(rep.ID)
	if err != nil {
		return nil, err
	}
	items := make([]DirGroupItem, 0, len(views))
	for _, v := range views {
		if len(v.Items) < 2 {
			continue
		}
		// 检测按类型分桶，组内成员类型一致，取首成员判断
		if kind != "" && kind != "all" && v.Items[0].Kind != kind {
			continue
		}
		mediaItems := make([]repo.MediaView, 0, len(v.Items))
		for _, it := range v.Items {
			mediaItems = append(mediaItems, it.MediaView)
		}
		items = append(items, DirGroupItem{
			ID:          v.ID,
			GroupType:   v.GroupType,
			MemberCount: len(v.Items),
			FreedBytes:  groupFreedBytes(mediaItems),
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
	return &DirGroupPage{
		Total:      total,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
		Items:      items[start:end],
	}, nil
}

func reasonToGroupType(reason string) string {
	switch reason {
	case "sha1_exact":
		return "sha1"
	case "phash_similar":
		return "image_similar"
	case "sprite_phash_similar":
		return "video_similar"
	case "oshash_short_exact":
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
		// 报告组只要剩下 0/1 个成员，就不再属于重复组。
		// 这里再次防御性过滤，避免清理后的瞬间或历史脏数据把单张图片返回给 UI。
		if len(v.Items) < 2 {
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
// kind 为 all/image/video：指定时只统计该类型媒体的重复成员，与 Groups 的口径一致，
// 否则目录树会包含另一类型媒体独占的目录（前端按类型过滤后点击无数据）。
func (s *Service) Tree(kind string) ([]*TreeItem, error) {
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
		if kind != "" && kind != "all" && len(v.Items) > 0 && v.Items[0].Kind != kind {
			continue
		}
		// 先按组内目录分桶。same_dir 报告要求目录内至少两个成员；
		// all 报告则只要目录包含该全局重复组的成员，就必须显示该目录。
		membersByDir := map[string]int{}
		for _, m := range v.Items {
			membersByDir[relDir(m.RelativePath)]++
		}
		for dir, count := range membersByDir {
			if rep.Scope == "all" || count >= 2 {
				dirs[dir] += count
			}
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
	// 直属文件数只来源于实际包含重复成员的目录。
	for dir, cnt := range dirs {
		if dir == "." || dir == "" {
			continue
		}
		if node, ok := nodes[dir]; ok {
			node.FileCount += cnt
		}
	}

	// 所有节点都要参与挂接；中间目录即使没有直属文件，也不能从树中断开。
	roots := make([]*TreeItem, 0, len(nodes))
	for path, node := range nodes {
		parent := parentDir(path)
		if p, ok := nodes[parent]; ok {
			p.Children = append(p.Children, node)
			continue
		}
		roots = append(roots, node)
	}
	for _, n := range nodes {
		sort.Slice(n.Children, func(i, j int) bool {
			return n.Children[i].Path < n.Children[j].Path
		})
	}
	sort.Slice(roots, func(i, j int) bool {
		return roots[i].Path < roots[j].Path
	})
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

// ExcludeMedia 人工排除重复：将该文件从当前报告中移除（仅对当前报告生效，
// 重新生成报告后该文件重新参与检测）。删除其全部组成员关系，并清理因此
// 成员数 <2 的组、刷新报告统计；返回移除的成员数（不在报告中时返回 0）。
func (s *Service) ExcludeMedia(mediaID int64) (int64, error) {
	n, err := s.Dup.DeleteMembersByMedia(mediaID)
	if err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, nil // 文件不在报告中，幂等
	}
	if err := s.Dup.PruneGroupsAndUpdateStats(); err != nil {
		return 0, err
	}
	return n, nil
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
	toDelete := make([]int64, 0)
	switch req.Scope {
	case "directory":
		// 删除目标目录下的所有重复数据：按"本目录成员"为单位处理，绝不删除其它目录的成员。
		// 组内本目录成员 >=2（本目录内互相重复）→ 按保留条件在本目录成员中保留 1 个，删其余；
		// 组内本目录成员 ==1（仅与其它目录文件重复）→ 直接删除本目录这一份。
		targetDir := normalizeDirectory(req.Directory)
		for _, v := range views {
			if len(v.Items) == 0 {
				continue
			}
			local := make([]repo.MediaView, 0, len(v.Items))
			for _, m := range v.Items {
				if relDir(m.RelativePath) == targetDir {
					local = append(local, m)
				}
			}
			if len(local) == 0 {
				continue
			}
			if len(local) >= 2 {
				keepIdx := keepIndex(local, req.Keep)
				for i, m := range local {
					if i != keepIdx {
						toDelete = append(toDelete, m.ID)
					}
				}
			} else {
				toDelete = append(toDelete, local[0].ID)
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

// DirTree 返回最新目录对比报告中包含重复文件的目录树；kind 非空时按媒体类型过滤。
// 与重复报告的 Tree() 语义不同：目录对比的组是"目标 vs 存量"，同一目录内通常只有
// 1 个成员，因此只要目录内存在重复组成员（≥1）即计入，file_count 为该目录内成员数，
// 展示所有涉及重复的目录；重复报告 Tree() 的"目录内 ≥2 才算"是目录内重复语义。
func (s *Service) DirTree(kind string) ([]*TreeItem, error) {
	if s.Dir == nil {
		return []*TreeItem{}, nil
	}
	rep, err := s.Dir.GetLatestDirReport()
	if err != nil {
		return nil, err
	}
	if rep == nil {
		return []*TreeItem{}, nil
	}
	views, err := s.Dir.DirGroupViews(rep.ID)
	if err != nil {
		return nil, err
	}
	dirs := map[string]int{}
	for _, v := range views {
		if kind != "" && kind != "all" && len(v.Items) > 0 && v.Items[0].Kind != kind {
			continue
		}
		// 按目录统计重复组成员；目录内 1 个成员也计入（目录对比语义）
		membersByDir := map[string]int{}
		for _, m := range v.Items {
			membersByDir[relDir(m.RelativePath)]++
		}
		for dir, count := range membersByDir {
			dirs[dir] += count
		}
	}
	return buildDirTree(dirs), nil
}

// DirExcludeMedia 从最新目录对比报告中排除指定媒体（人工判定无重复，仅当前报告生效，
// 重新生成后该文件重新参与检测）。删除其全部组成员关系并清理 <2 的组、刷新统计。
func (s *Service) DirExcludeMedia(mediaID int64) (int64, error) {
	if s.Dir == nil {
		return 0, nil
	}
	n, err := s.Dir.DeleteMembersByMedia(mediaID)
	if err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, nil // 文件不在报告中，幂等
	}
	if err := s.Dir.PruneGroupsAndUpdateStats(); err != nil {
		return 0, err
	}
	return n, nil
}

// DirClear 一键清除目录对比重复文件：每组按保留条件保留 1 个，其余删除
// （含缩略图/media 记录/本地文件），并刷新目录对比报告统计。
func (s *Service) DirClear(req ClearRequest, permanent bool) (*ClearResult, error) {
	if s.Dir == nil {
		return &ClearResult{}, nil
	}
	rep, err := s.Dir.GetLatestDirReport()
	if err != nil {
		return nil, err
	}
	if rep == nil {
		return nil, fmt.Errorf("尚无目录对比报告")
	}
	views, err := s.Dir.DirGroupViews(rep.ID)
	if err != nil {
		return nil, err
	}
	targets := make([]repo.DirGroupView, 0, len(views))
	switch req.Scope {
	case "directory":
		targetDir := normalizeDirectory(req.Directory)
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
			if allSame && d == targetDir {
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
		mediaItems := make([]repo.MediaView, 0, len(v.Items))
		for _, it := range v.Items {
			mediaItems = append(mediaItems, it.MediaView)
		}
		keepIdx := keepIndex(mediaItems, req.Keep)
		for i, m := range v.Items {
			if i != keepIdx {
				toDelete = append(toDelete, m.ID)
			}
		}
	}
	if len(toDelete) == 0 {
		remaining := 0
		if latest, _ := s.Dir.GetLatestDirReport(); latest != nil {
			remaining = latest.TotalGroups
		}
		return &ClearResult{RemainingGroups: remaining}, nil
	}
	res, err := s.DeleteMedia(toDelete, permanent)
	if err != nil {
		return nil, err
	}
	remaining := 0
	if latest, _ := s.Dir.GetLatestDirReport(); latest != nil {
		remaining = latest.TotalGroups
	}
	return &ClearResult{DeletedFiles: res.DeletedFiles, FreedBytes: res.FreedBytes, RemainingGroups: remaining}, nil
}

// normalizeDirectory 统一前端根目录空字符串与后端 relDir 的“.”表示。
func normalizeDirectory(dir string) string {
	if strings.TrimSpace(dir) == "" {
		return "."
	}
	dir = strings.ReplaceAll(dir, "\\", "/")
	return strings.Trim(dir, "/")
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

// PruneAfterMediaChange 删除类变更后清理重复组并刷新统计（普通报告 + 目录对比报告）。
func (s *Service) PruneAfterMediaChange() error {
	if err := s.Dup.PruneGroupsAndUpdateStats(); err != nil {
		return err
	}
	if s.Dir != nil {
		if err := s.Dir.PruneGroupsAndUpdateStats(); err != nil {
			return err
		}
	}
	return nil
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
