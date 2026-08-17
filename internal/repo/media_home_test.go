// media_home_test.go：首页媒体查询仓储测试。
// 代码注释使用中文。
package repo

import (
	"testing"
	"time"
)

func setupMediaHomeRepo(t *testing.T) (*MediaRepo, *Library, *Library) {
	t.Helper()
	d := newTestDB(t)
	lr := NewLibraryRepo(d)
	a := &Library{Name: "A库", Path: "C:/A", Kind: "mixed"}
	b := &Library{Name: "B库", Path: "D:/B", Kind: "mixed"}
	if err := lr.Create(a); err != nil {
		t.Fatal(err)
	}
	if err := lr.Create(b); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO scan_sessions(id, library_id, is_temporary, status) VALUES ('tmp-home', ?, 1, 'completed')`, a.ID); err != nil {
		t.Fatal(err)
	}
	mr := NewMediaRepo(d)
	base := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	duration := int64(5000)
	items := []Media{
		{LibraryID: a.ID, Kind: "image", RelativePath: "root.jpg", FileSize: 10, Mtime: base.Add(time.Hour)},
		{LibraryID: a.ID, Kind: "image", RelativePath: "旅行/一月/a.jpg", FileSize: 20, Mtime: base.Add(4 * time.Hour)},
		{LibraryID: a.ID, Kind: "image", RelativePath: "旅行/二月/b.jpg", FileSize: 30, Mtime: base.Add(3 * time.Hour)},
		{LibraryID: b.ID, Kind: "video", RelativePath: "旅行/c.mp4", FileSize: 40, Mtime: base.Add(2 * time.Hour), DurationMs: &duration},
	}
	for i := range items {
		if err := mr.Upsert(&items[i]); err != nil {
			t.Fatal(err)
		}
	}
	tmp := "tmp-home"
	if err := mr.Upsert(&Media{LibraryID: a.ID, ScanSessionID: &tmp, Kind: "image", RelativePath: "旅行/tmp.jpg", FileSize: 999, Mtime: base.Add(10 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	return mr, a, b
}

func TestListFormalPageExcludesTemporaryAndOrders(t *testing.T) {
	mr, _, _ := setupMediaHomeRepo(t)
	page, err := mr.ListFormalPage("image", 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 3 || page.TotalPages != 2 || len(page.Items) != 2 {
		t.Fatalf("分页结果错误: %+v", page)
	}
	if page.Items[0].RelativePath != "旅行/一月/a.jpg" || page.Items[1].RelativePath != "旅行/二月/b.jpg" {
		t.Fatalf("分页顺序错误: %+v", page.Items)
	}
}

func TestListFormalGroupsAndStatistics(t *testing.T) {
	mr, a, b := setupMediaHomeRepo(t)
	groups, total, err := mr.ListFormalGroups(1, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || len(groups) != 3 {
		t.Fatalf("分组数量=%d/%d, want 3/3", len(groups), total)
	}
	if groups[0].LibraryID != a.ID || groups[0].GroupPath != "旅行" || groups[0].Total != 2 || len(groups[0].Items) != 2 {
		t.Fatalf("首组应包含 A 库旅行目录全部后代: %+v", groups[0])
	}
	if groups[1].LibraryID != b.ID || groups[1].GroupPath != "旅行" {
		t.Fatalf("跨库分组错误: %+v", groups[1])
	}
	if groups[2].GroupPath != "" || groups[2].Items[0].RelativePath != "root.jpg" {
		t.Fatalf("根目录分组错误: %+v", groups[2])
	}
	lazy, lazyTotal, err := mr.ListFormalGroups(1, 1, 1)
	if err != nil || lazyTotal != 3 || len(lazy) != 1 || lazy[0].LibraryID != b.ID {
		t.Fatalf("组懒加载错误: %+v total=%d err=%v", lazy, lazyTotal, err)
	}

	stats, err := mr.FormalStatistics()
	if err != nil {
		t.Fatal(err)
	}
	if stats.Image.Count != 3 || stats.Image.Size != 60 || stats.Video.Count != 1 || stats.Video.Size != 40 || stats.Video.DurationMs != 5000 || stats.TotalSize != 100 {
		t.Fatalf("统计错误: %+v", stats)
	}
}

// TestListFormalGroupsByQuery 搜索命中：整组返回、多组同时返回、大小写不敏感、临时媒体排除。
func TestListFormalGroupsByQuery(t *testing.T) {
	mr, a, b := setupMediaHomeRepo(t)

	// "jpg" 命中 root.jpg（A库根）、一月/a.jpg、二月/b.jpg → A库根 + A库旅行 两组；
	// 临时文件 tmp.jpg 排除；B库旅行/c.mp4 不命中。
	groups, total, err := mr.ListFormalGroupsByQuery(1, "jpg", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(groups) != 2 {
		t.Fatalf("命中组数=%d/%d, want 2/2", len(groups), total)
	}
	// 整组返回：A 库旅行组 items 含整组后代（一月+二月），而非只命中文件
	var travel *MediaGroup
	var root *MediaGroup
	for i := range groups {
		if groups[i].GroupPath == "旅行" {
			travel = &groups[i]
		} else if groups[i].GroupPath == "" {
			root = &groups[i]
		}
	}
	if travel == nil || root == nil {
		t.Fatalf("应同时返回旅行与根分组: %+v", groups)
	}
	if travel.LibraryID != a.ID || travel.Total != 2 || len(travel.Items) != 2 {
		t.Fatalf("命中组应整组返回（含全部后代）: %+v", travel)
	}
	if root.Items[0].RelativePath != "root.jpg" {
		t.Fatalf("根分组命中内容错误: %+v", root)
	}
	// 临时媒体不应出现
	for _, g := range groups {
		for _, it := range g.Items {
			if it.RelativePath == "旅行/tmp.jpg" {
				t.Fatalf("临时媒体不应出现在结果: %+v", it)
			}
		}
	}

	// 大小写不敏感
	groups, total, err = mr.ListFormalGroupsByQuery(1, "JPG", 0, 10)
	if err != nil || total != 2 {
		t.Fatalf("大小写不敏感搜索失败 total=%d err=%v", total, err)
	}

	// 无命中
	groups, total, err = mr.ListFormalGroupsByQuery(1, "不存在的文件", 0, 10)
	if err != nil || total != 0 || len(groups) != 0 {
		t.Fatalf("无命中应返回空: %d/%d err=%v", len(groups), total, err)
	}

	// 命中一个分组：b 库 旅行/c.mp4
	groups, total, err = mr.ListFormalGroupsByQuery(1, "c.mp4", 0, 10)
	if err != nil || total != 1 || len(groups) != 1 {
		t.Fatalf("单组命中失败: %d/%d err=%v", len(groups), total, err)
	}
	if groups[0].LibraryID != b.ID || groups[0].Items[0].RelativePath != "旅行/c.mp4" {
		t.Fatalf("单组命中内容错误: %+v", groups[0])
	}

	// 分页：limit=1 取第一组，total 仍为 2
	groups, total, err = mr.ListFormalGroupsByQuery(1, "jpg", 0, 1)
	if err != nil || total != 2 || len(groups) != 1 {
		t.Fatalf("搜索分页错误: %d/%d err=%v", len(groups), total, err)
	}

	// q 为空回退全量
	groups, total, err = mr.ListFormalGroupsByQuery(1, "  ", 0, 10)
	if err != nil || total != 3 {
		t.Fatalf("空 q 应回退全量: %d err=%v", total, err)
	}
}
