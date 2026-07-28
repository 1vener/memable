// repo 包测试：内存库验证各 Repository CRUD。
// 代码注释使用中文。
package repo

import (
	"database/sql"
	"testing"
	"time"

	"memable/internal/config"
	"memable/internal/db"
	"memable/internal/errx"
)

// newTestDB 建立内存库并执行迁移。
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	cfg := &config.Config{Database: config.DatabaseConfig{Path: ":memory:"}}
	dbh, err := db.Open(cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := db.Migrate(dbh); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	t.Cleanup(func() { dbh.Close() })
	return dbh
}

func TestLibraryCRUD(t *testing.T) {
	d := newTestDB(t)
	r := NewLibraryRepo(d)

	l := &Library{Name: "照片库", Path: "D:/Pictures", Kind: "image"}
	if err := r.Create(l); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if l.ID == 0 {
		t.Fatal("Create 后 ID 为空")
	}

	got, err := r.GetByID(l.ID)
	if err != nil || got.Name != "照片库" {
		t.Fatalf("GetByID: %+v %v", got, err)
	}

	if err := r.UpdatePath(l.ID, "E:/Pictures"); err != nil {
		t.Fatalf("UpdatePath: %v", err)
	}
	got, _ = r.GetByID(l.ID)
	if got.Path != "E:/Pictures" {
		t.Fatalf("迁移后路径未生效: %s", got.Path)
	}

	list, err := r.List()
	if err != nil || len(list) != 1 {
		t.Fatalf("List: %d %v", len(list), err)
	}

	if err := r.Delete(l.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := r.GetByID(l.ID); err == nil {
		t.Fatal("删除后仍可查询到")
	}
}

func TestSessionLifecycle(t *testing.T) {
	d := newTestDB(t)
	lr := NewLibraryRepo(d)
	sr := NewSessionRepo(d)

	l := &Library{Name: "视频库", Path: "E:/Videos", Kind: "video"}
	if err := lr.Create(l); err != nil {
		t.Fatal(err)
	}

	s := &ScanSession{ID: "sess-001", LibraryID: &l.ID, IsTemporary: true, Status: "running"}
	if err := sr.Create(s); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := sr.GetByID("sess-001")
	if err != nil || !got.IsTemporary || got.Status != "running" {
		t.Fatalf("GetByID: %+v %v", got, err)
	}

	if err := sr.UpdateStatus("sess-001", "completed"); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	got, _ = sr.GetByID("sess-001")
	if got.FinishedAt == nil {
		t.Fatal("终态未写入 finished_at")
	}

	if err := sr.Promote("sess-001"); err != nil {
		t.Fatalf("Promote: %v", err)
	}
	got, _ = sr.GetByID("sess-001")
	if got.IsTemporary || got.Status != "promoted" {
		t.Fatalf("入库后状态错误: %+v", got)
	}
}

func TestMediaNeedScan(t *testing.T) {
	d := newTestDB(t)
	lr := NewLibraryRepo(d)
	mr := NewMediaRepo(d)

	l := &Library{Name: "照片库", Path: "D:/Pictures", Kind: "image"}
	if err := lr.Create(l); err != nil {
		t.Fatal(err)
	}

	mt := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	m := &Media{LibraryID: l.ID, Kind: "image", RelativePath: "a/b.jpg", FileSize: 1024, Mtime: mt}
	if err := mr.Upsert(m); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	// 完全一致 -> 跳过
	need, err := mr.NeedScan(l.ID, "a/b.jpg", mt, 1024)
	if err != nil || need {
		t.Fatalf("未变化应跳过: need=%v err=%v", need, err)
	}

	// size 变化 -> 重扫
	need, _ = mr.NeedScan(l.ID, "a/b.jpg", mt, 2048)
	if !need {
		t.Fatal("size 变化应重扫")
	}

	// mtime 变化 -> 重扫
	need, _ = mr.NeedScan(l.ID, "a/b.jpg", mt.Add(time.Hour), 1024)
	if !need {
		t.Fatal("mtime 变化应重扫")
	}

	// 新路径 -> 重扫
	need, _ = mr.NeedScan(l.ID, "a/c.jpg", mt, 1024)
	if !need {
		t.Fatal("新文件应重扫")
	}
}

func TestMediaSearchAndSha1(t *testing.T) {
	d := newTestDB(t)
	lr := NewLibraryRepo(d)
	mr := NewMediaRepo(d)

	l := &Library{Name: "照片库", Path: "D:/Pictures", Kind: "image"}
	if err := lr.Create(l); err != nil {
		t.Fatal(err)
	}

	sha := "da39a3ee5e6b4b0d3255bfef95601890afd80709"
	mt := time.Now().UTC()
	for _, p := range []string{"2024/a.jpg", "2025/b.jpg"} {
		m := &Media{LibraryID: l.ID, Kind: "image", RelativePath: p, FileSize: 1, Mtime: mt, Sha1: &sha}
		if err := mr.Upsert(m); err != nil {
			t.Fatal(err)
		}
	}

	dups, err := mr.FindBySha1(sha)
	if err != nil || len(dups) != 2 {
		t.Fatalf("FindBySha1: %d %v", len(dups), err)
	}

	hits, err := mr.SearchByPath("2025")
	if err != nil || len(hits) != 1 {
		t.Fatalf("SearchByPath: %d %v", len(hits), err)
	}
}

func TestFrameCRUD(t *testing.T) {
	d := newTestDB(t)
	lr := NewLibraryRepo(d)
	mr := NewMediaRepo(d)
	fr := NewFrameRepo(d)

	l := &Library{Name: "视频库", Path: "E:/Videos", Kind: "video"}
	if err := lr.Create(l); err != nil {
		t.Fatal(err)
	}
	m := &Media{LibraryID: l.ID, Kind: "video", RelativePath: "movie.mp4", FileSize: 1, Mtime: time.Now().UTC()}
	if err := mr.Upsert(m); err != nil {
		t.Fatal(err)
	}

	ph := "abcd1234"
	for i := 1; i <= 9; i++ {
		f := &VideoFrame{MediaID: m.ID, FrameIndex: i, SampleRatio: float64(i*10-5) / 100,
			TimeMs: int64(i * 1000), Phash: &ph}
		if err := fr.Upsert(f); err != nil {
			t.Fatalf("Upsert frame %d: %v", i, err)
		}
	}

	frames, err := fr.ListByMedia(m.ID)
	if err != nil || len(frames) != 9 {
		t.Fatalf("ListByMedia: %d %v", len(frames), err)
	}
	if frames[4].FrameIndex != 5 {
		t.Fatalf("第5帧排序错误: %+v", frames[4])
	}

	if err := fr.DeleteByMedia(m.ID); err != nil {
		t.Fatal(err)
	}
	frames, _ = fr.ListByMedia(m.ID)
	if len(frames) != 0 {
		t.Fatal("删除后仍有帧")
	}
}

func TestWithTxRollback(t *testing.T) {
	d := newTestDB(t)
	lr := NewLibraryRepo(d)

	err := WithTx(d, 3, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO libraries (name, path, kind) VALUES ('t','/tmp','image')`); err != nil {
			return err
		}
		return errx.Newf("强制失败")
	})
	if err == nil {
		t.Fatal("事务应返回错误")
	}

	list, _ := lr.List()
	if len(list) != 0 {
		t.Fatal("回滚后数据未清理")
	}
}
