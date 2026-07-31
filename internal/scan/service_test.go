// scan 包测试：扫描服务与 Repository 集成。
// 代码注释使用中文。
package scan

import (
	"context"
	"database/sql"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"memable/internal/config"
	"memable/internal/db"
	"memable/internal/repo"
)

func TestScanLibraryImageIncremental(t *testing.T) {
	dbh := newTestDB(t)
	lr := repo.NewLibraryRepo(dbh)
	mr := repo.NewMediaRepo(dbh)
	sr := repo.NewSessionRepo(dbh)

	dir := t.TempDir()
	imgPath := filepath.Join(dir, "photos", "one.png")
	if err := os.MkdirAll(filepath.Dir(imgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	writePNG(t, imgPath, 10, 7)

	lib := &repo.Library{Name: "照片库", Path: dir, Kind: "image"}
	if err := lr.Create(lib); err != nil {
		t.Fatal(err)
	}

	svc := &Service{Sessions: sr, Media: mr}
	stats, err := svc.ScanLibrary(context.Background(), *lib, "scan-1", false)
	if err != nil {
		t.Fatalf("ScanLibrary: %v", err)
	}
	if stats.Found != 1 || stats.Imported != 1 || stats.Skipped != 0 {
		t.Fatalf("第一次扫描统计错误: %+v", stats)
	}

	m, err := mr.GetByPath(lib.ID, "photos/one.png")
	if err != nil || m == nil {
		t.Fatalf("GetByPath: %+v %v", m, err)
	}
	if m.Width == nil || *m.Width != 10 || m.Height == nil || *m.Height != 7 || m.Sha1 == nil || len(*m.Sha1) != 40 {
		t.Fatalf("媒体 metadata 错误: %+v", m)
	}
	if m.Phash == nil || len(*m.Phash) != 16 || m.Dhash == nil || len(*m.Dhash) != 16 || m.Ahash == nil || len(*m.Ahash) != 16 {
		t.Fatalf("图片相似哈希未写入: %+v", m)
	}

	stats, err = svc.ScanLibrary(context.Background(), *lib, "scan-2", false)
	if err != nil {
		t.Fatalf("第二次扫描: %v", err)
	}
	if stats.Imported != 0 || stats.Skipped != 1 {
		t.Fatalf("增量跳过统计错误: %+v", stats)
	}

	// 修改文件内容并确保 mtime 变化，下一次应重新导入。
	time.Sleep(1100 * time.Millisecond)
	writePNG(t, imgPath, 11, 7)
	stats, err = svc.ScanLibrary(context.Background(), *lib, "scan-3", false)
	if err != nil {
		t.Fatalf("第三次扫描: %v", err)
	}
	if stats.Imported != 1 || stats.Skipped != 0 {
		t.Fatalf("修改后应重扫: %+v", stats)
	}
}

func TestExecuteScanRepairsMissingThumbnailAndSupportsForce(t *testing.T) {
	dbh := newTestDB(t)
	lr := repo.NewLibraryRepo(dbh)
	mr := repo.NewMediaRepo(dbh)
	sr := repo.NewSessionRepo(dbh)

	dir := t.TempDir()
	thumbDir := t.TempDir()
	imgPath := filepath.Join(dir, "one.png")
	writePNG(t, imgPath, 10, 7)
	lib := &repo.Library{Name: "同步库", Path: dir, Kind: "image"}
	if err := lr.Create(lib); err != nil {
		t.Fatal(err)
	}
	svc := &Service{Sessions: sr, Media: mr, ImageThumbBase: thumbDir}
	progress := repo.ProgressFunc(func(string, int, int, int, int, int, float64, *int64) {})

	result, err := svc.ExecuteScan(context.Background(), *lib, "sync-1", false, false, 1, progress)
	if err != nil || result.Imported != 1 {
		t.Fatalf("首次同步扫描错误: result=%+v err=%v", result, err)
	}
	stored, err := mr.GetByPath(lib.ID, "one.png")
	if err != nil || stored == nil || stored.ThumbnailPath == nil {
		t.Fatalf("媒体或缩略图未写入: media=%+v err=%v", stored, err)
	}
	thumbPath := filepath.Join(thumbDir, filepath.FromSlash(*stored.ThumbnailPath))
	if err := os.Remove(thumbPath); err != nil {
		t.Fatal(err)
	}

	result, err = svc.ExecuteScan(context.Background(), *lib, "sync-2", false, false, 1, progress)
	if err != nil || result.Imported != 1 || result.Skipped != 0 {
		t.Fatalf("缩略图丢失后应重新处理: result=%+v err=%v", result, err)
	}
	if _, err := os.Stat(thumbPath); err != nil {
		t.Fatalf("缩略图未修复: %v", err)
	}

	result, err = svc.ExecuteScan(context.Background(), *lib, "sync-3", false, true, 1, progress)
	if err != nil || result.Imported != 1 || result.Skipped != 0 {
		t.Fatalf("强制同步应重新处理未变化文件: result=%+v err=%v", result, err)
	}
}

func TestExecuteScanCleansMissingMediaAndThumbnail(t *testing.T) {
	dbh := newTestDB(t)
	lr := repo.NewLibraryRepo(dbh)
	mr := repo.NewMediaRepo(dbh)
	sr := repo.NewSessionRepo(dbh)

	dir := t.TempDir()
	thumbDir := t.TempDir()
	imgPath := filepath.Join(dir, "gone.png")
	writePNG(t, imgPath, 8, 8)
	lib := &repo.Library{Name: "清理库", Path: dir, Kind: "image"}
	if err := lr.Create(lib); err != nil {
		t.Fatal(err)
	}
	svc := &Service{Sessions: sr, Media: mr, ImageThumbBase: thumbDir}
	progress := repo.ProgressFunc(func(string, int, int, int, int, int, float64, *int64) {})

	if _, err := svc.ExecuteScan(context.Background(), *lib, "clean-1", false, false, 1, progress); err != nil {
		t.Fatal(err)
	}
	stored, err := mr.GetByPath(lib.ID, "gone.png")
	if err != nil || stored == nil || stored.ThumbnailPath == nil {
		t.Fatalf("媒体未写入: media=%+v err=%v", stored, err)
	}
	thumbPath := filepath.Join(thumbDir, filepath.FromSlash(*stored.ThumbnailPath))
	if err := os.Remove(imgPath); err != nil {
		t.Fatal(err)
	}

	result, err := svc.ExecuteScan(context.Background(), *lib, "clean-2", false, false, 1, progress)
	if err != nil || result.Cleaned != 1 {
		t.Fatalf("缺失媒体清理错误: result=%+v err=%v", result, err)
	}
	stored, err = mr.GetByPath(lib.ID, "gone.png")
	if err != nil || stored != nil {
		t.Fatalf("本地缺失记录仍存在: media=%+v err=%v", stored, err)
	}
	if _, err := os.Stat(thumbPath); !os.IsNotExist(err) {
		t.Fatalf("失效缩略图未删除: %v", err)
	}
}

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dbh, err := db.Open(&config.Config{Database: config.DatabaseConfig{Path: ":memory:"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(dbh); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dbh.Close() })
	return dbh
}

func writePNG(t *testing.T, path string, w, h int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(w), G: uint8(h), B: 1, A: 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}
