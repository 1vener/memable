// media_repo_test.go：media 表目录前缀批量更新测试。
// 代码注释使用中文。
package repo

import (
	"testing"
	"time"

	"memable/internal/config"
	"memable/internal/db"
)

func setupDirPrefixRepo(t *testing.T) (*MediaRepo, int64) {
	t.Helper()
	dbh, err := db.Open(&config.Config{Database: config.DatabaseConfig{Path: ":memory:"}})
	if err != nil {
		t.Fatalf("打开测试数据库: %v", err)
	}
	t.Cleanup(func() { _ = dbh.Close() })
	if err := db.Migrate(dbh); err != nil {
		t.Fatalf("迁移测试数据库: %v", err)
	}
	lr := NewLibraryRepo(dbh)
	lib := &Library{Name: "t", Path: "C:/media", Kind: "mixed"}
	if err := lr.Create(lib); err != nil {
		t.Fatalf("创建库: %v", err)
	}
	mr := NewMediaRepo(dbh)
	now := time.Now().UTC()
	for _, rel := range []string{
		"a/1.jpg",
		"a/b/2.jpg",
		"a/bc/3.jpg", // a/bc 与 a/b 是不同目录，前缀更新时不得误伤
		"other/4.jpg",
	} {
		if err := mr.Upsert(&Media{LibraryID: lib.ID, Kind: "image", RelativePath: rel, FileSize: 1, Mtime: now}); err != nil {
			t.Fatalf("写入媒体 %q: %v", rel, err)
		}
	}
	return mr, lib.ID
}

func rels(t *testing.T, mr *MediaRepo, libID int64) []string {
	t.Helper()
	list, err := mr.ListByLibrary(libID)
	if err != nil {
		t.Fatalf("查询媒体: %v", err)
	}
	out := make([]string, 0, len(list))
	for _, m := range list {
		out = append(out, m.RelativePath)
	}
	return out
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// TestRenameDirectoryPrefix 验证目录前缀批量更新：目录内文件、子目录文件更新，
// 名称前缀相似但目录不同的路径（a/bc）不被误伤，目录外路径不变。
func TestRenameDirectoryPrefix(t *testing.T) {
	mr, libID := setupDirPrefixRepo(t)

	n, err := mr.RenameDirectoryPrefix(libID, "a", "x/y")
	if err != nil {
		t.Fatalf("RenameDirectoryPrefix: %v", err)
	}
	if n != 3 {
		t.Fatalf("受影响行数 = %d, want 3", n)
	}
	got := rels(t, mr, libID)
	want := []string{"other/4.jpg", "x/y/1.jpg", "x/y/b/2.jpg", "x/y/bc/3.jpg"}
	for _, w := range want {
		if !contains(got, w) {
			t.Fatalf("缺少路径 %q，实际 %v", w, got)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("路径数量 = %d, want %d，实际 %v", len(got), len(want), got)
	}

	// 再对子目录做一次（子前缀也支持）
	n, err = mr.RenameDirectoryPrefix(libID, "x/y/b", "p")
	if err != nil {
		t.Fatalf("第二次 RenameDirectoryPrefix: %v", err)
	}
	if n != 1 {
		t.Fatalf("子目录受影响行数 = %d, want 1", n)
	}
	got = rels(t, mr, libID)
	if !contains(got, "p/2.jpg") {
		t.Fatalf("缺少路径 p/2.jpg，实际 %v", got)
	}
}

// TestRenameDirectoryPrefixEmptyOld 旧前缀为空应报错。
func TestRenameDirectoryPrefixEmptyOld(t *testing.T) {
	mr, libID := setupDirPrefixRepo(t)
	if _, err := mr.RenameDirectoryPrefix(libID, "", "x"); err == nil {
		t.Fatal("空旧前缀应报错")
	}
}

// TestListDirChildren 验证目录树由 media.relative_path 派生：
// 各级子目录集合、has_children（是否含更深层级）、根级文件不误判为目录、
// 空 parent（根）与深层 parent 两种路径形态。
func TestListDirChildren(t *testing.T) {
	mr, libID := setupDirPrefixRepo(t)
	// 追加根级文件与更深层级、以及含"相似前缀"兄弟目录的路径
	now := time.Now().UTC()
	for _, rel := range []string{
		"root.jpg",        // 根级文件：不得出现在任何目录层级
		"a/b/c/deep.jpg",  // a/b 有更深层级 → has_children=true
		"other/sub/5.jpg", // other 有子目录 sub
		"other/x.jpg",     // other 直属文件
		"onlyfile/6.jpg",  // onlyfile 只有直属文件，无子目录
	} {
		if err := mr.Upsert(&Media{LibraryID: libID, Kind: "image", RelativePath: rel, FileSize: 1, Mtime: now}); err != nil {
			t.Fatalf("写入媒体 %q: %v", rel, err)
		}
	}

	assertChildren := func(t *testing.T, parent string, want map[string]bool) {
		t.Helper()
		children, err := mr.ListDirChildren(libID, parent)
		if err != nil {
			t.Fatalf("ListDirChildren(%q): %v", parent, err)
		}
		if len(children) != len(want) {
			t.Fatalf("ListDirChildren(%q) 数量 = %d, want %d，实际 %+v", parent, len(children), len(want), children)
		}
		for _, c := range children {
			has, ok := want[c.Name]
			if !ok {
				t.Fatalf("ListDirChildren(%q) 意外子目录 %q", parent, c.Name)
			}
			if c.HasChildren != has {
				t.Fatalf("子目录 %q has_children = %v, want %v", c.Name, c.HasChildren, has)
			}
		}
	}

	// 根目录：a（有子目录 b/bc）、other（有子目录 sub）、onlyfile（只有直属文件）；
	// root.jpg（根级文件）不出现
	assertChildren(t, "", map[string]bool{"a": true, "other": true, "onlyfile": false})
	// a：b（其下有 c，更深层级）、bc（只有直属文件）
	assertChildren(t, "a", map[string]bool{"b": true, "bc": false})
	// a/b：c（c/deep.jpg 无更深的目录层级）
	assertChildren(t, "a/b", map[string]bool{"c": false})
	// other：sub（只有直属文件 5.jpg）
	assertChildren(t, "other", map[string]bool{"sub": false})
	// onlyfile：无子目录
	assertChildren(t, "onlyfile", map[string]bool{})
	// 带前导/尾随斜杠的 parent 也能解析
	assertChildren(t, "/a/", map[string]bool{"b": true, "bc": false})

	// 中文/emoji 目录名回归：SQLite substr/instr 按字符计数，Go len 按字节计数，
	// offset 必须用字符数，否则中文父目录的子项被切错位置（曾返回空/截断名）
	for _, rel := range []string{
		"情侣/美女与男友日常自拍/视频.mp4", // 情侣 → 美女与男友日常自拍（无更深）
		"日期/【合集】❤️精选/1.jpg",   // 日期 → 【合集】❤️精选（有更深层 1.jpg 的目录）
		"日期/【合集】❤️精选/子目录/2.jpg",
	} {
		if err := mr.Upsert(&Media{LibraryID: libID, Kind: "video", RelativePath: rel, FileSize: 1, Mtime: now}); err != nil {
			t.Fatalf("写入媒体 %q: %v", rel, err)
		}
	}
	assertChildren(t, "情侣", map[string]bool{"美女与男友日常自拍": false})
	assertChildren(t, "日期", map[string]bool{"【合集】❤️精选": true})
	assertChildren(t, "日期/【合集】❤️精选", map[string]bool{"子目录": false})
}

// TestSearchLibraries 验证库内文件名/目录名搜索的目录级聚合语义：
// 目录名命中直接返回该目录；文件名命中汇总父目录并计数；根级文件父目录为空串；
// 跨多个库返回（带库名）；临时扫描库排除；排序为 库名→目录路径（dir 类型在前）。
func TestSearchLibraries(t *testing.T) {
	dbh, err := db.Open(&config.Config{Database: config.DatabaseConfig{Path: ":memory:"}})
	if err != nil {
		t.Fatalf("打开测试数据库: %v", err)
	}
	t.Cleanup(func() { _ = dbh.Close() })
	if err := db.Migrate(dbh); err != nil {
		t.Fatalf("迁移测试数据库: %v", err)
	}
	lr := NewLibraryRepo(dbh)
	libA := &Library{Name: "A库", Path: "C:/media", Kind: "mixed"}
	libB := &Library{Name: "B库", Path: "D:/影集", Kind: "mixed"}
	if err := lr.Create(libA); err != nil {
		t.Fatalf("创建库 A: %v", err)
	}
	if err := lr.Create(libB); err != nil {
		t.Fatalf("创建库 B: %v", err)
	}
	mr := NewMediaRepo(dbh)
	now := time.Now().UTC()
	upsert := func(libID int64, rel string, sessionID string) {
		t.Helper()
		var sid *string
		if sessionID != "" {
			sid = &sessionID
		}
		if err := mr.Upsert(&Media{LibraryID: libID, ScanSessionID: sid, Kind: "image", RelativePath: rel, FileSize: 1, Mtime: now}); err != nil {
			t.Fatalf("写入媒体 %q: %v", rel, err)
		}
	}
	for _, rel := range []string{"a/1.jpg", "a/b/2.jpg", "a/bc/3.jpg", "other/4.jpg", "root.jpg"} {
		upsert(libA.ID, rel, "")
	}
	for _, rel := range []string{"2024/新年.jpg", "2024/1月/海边.mp4"} {
		upsert(libB.ID, rel, "")
	}

	// 临时扫描库：media 带 is_temporary=1 会话，不应出现在结果中
	if _, err := dbh.Exec(`INSERT INTO scan_sessions (id, is_temporary, status) VALUES ('tmp1', 1, 'running')`); err != nil {
		t.Fatalf("插入临时会话: %v", err)
	}
	upsert(libA.ID, "tmpdir/临时文件.jpg", "tmp1")

	find := func(hits []LibrarySearchHit, libName, dir, mtype string) *LibrarySearchHit {
		t.Helper()
		for i := range hits {
			h := &hits[i]
			if h.LibraryName == libName && h.DirPath == dir && h.MatchType == mtype {
				return h
			}
		}
		return nil
	}

	// 1. 目录名命中：bc → 目录 a/bc
	hits, err := mr.SearchLibraries("bc", 0)
	if err != nil {
		t.Fatalf("SearchLibraries(bc): %v", err)
	}
	h := find(hits, "A库", "a/bc", "dir")
	if h == nil {
		t.Fatalf("bc 应命中目录 a/bc，实际 %+v", hits)
	}
	if h.DirName != "bc" {
		t.Fatalf("DirName = %q, want bc", h.DirName)
	}
	if len(hits) != 1 {
		t.Fatalf("bc 命中数量 = %d, want 1，实际 %+v", len(hits), hits)
	}

	// 2. 文件名命中：3 → 汇总父目录 a/bc，计数 1
	hits, err = mr.SearchLibraries("3", 0)
	if err != nil {
		t.Fatalf("SearchLibraries(3): %v", err)
	}
	h = find(hits, "A库", "a/bc", "file")
	if h == nil {
		t.Fatalf("3 应汇总到目录 a/bc，实际 %+v", hits)
	}
	if h.MatchCount != 1 {
		t.Fatalf("a/bc 命中计数 = %d, want 1", h.MatchCount)
	}

	// 3. 根级文件命中：root.jpg → 父目录为空串（库根）
	hits, err = mr.SearchLibraries("root", 0)
	if err != nil {
		t.Fatalf("SearchLibraries(root): %v", err)
	}
	h = find(hits, "A库", "", "file")
	if h == nil {
		t.Fatalf("root 应汇总到库根，实际 %+v", hits)
	}
	if h.DirName != "" {
		t.Fatalf("库根 DirName = %q, want 空串", h.DirName)
	}

	// 4. 跨库 + 临时库排除：jpg 命中 A库 5 个父目录（含库根）、B库 1 个，
	//    临时会话的 tmpdir 不出现
	hits, err = mr.SearchLibraries("jpg", 0)
	if err != nil {
		t.Fatalf("SearchLibraries(jpg): %v", err)
	}
	if find(hits, "A库", "tmpdir", "file") != nil || find(hits, "A库", "tmpdir", "dir") != nil {
		t.Fatalf("临时扫描库不应出现在结果中，实际 %+v", hits)
	}
	if find(hits, "A库", "a", "file") == nil || find(hits, "A库", "a/b", "file") == nil ||
		find(hits, "A库", "a/bc", "file") == nil || find(hits, "A库", "other", "file") == nil ||
		find(hits, "A库", "", "file") == nil || find(hits, "B库", "2024", "file") == nil {
		t.Fatalf("jpg 命中集合不完整，实际 %+v", hits)
	}
	// 排序：A库(文件按路径) 在 B库 前
	names := make([]string, 0, len(hits))
	for i := range hits {
		names = append(names, hits[i].LibraryName+"/"+hits[i].DirPath)
	}
	for i := 1; i < len(names); i++ {
		if names[i-1] > names[i] {
			t.Fatalf("结果未按库名+路径排序：%v", names)
		}
	}
}
