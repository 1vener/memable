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
