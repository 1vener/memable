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
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"memable/internal/config"
	"memable/internal/db"
	"memable/internal/media"
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

	svc := &Service{Sessions: sr, Media: mr, ImageThumbBase: t.TempDir()}
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

func TestScanVideoSkipsSHA1AndIncremental(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg 未安装，跳过视频扫描测试")
	}
	dbh := newTestDB(t)
	lr := repo.NewLibraryRepo(dbh)
	mr := repo.NewMediaRepo(dbh)
	sr := repo.NewSessionRepo(dbh)

	dir := t.TempDir()
	videoPath := filepath.Join(dir, "sample.mp4")
	out, err := exec.Command("ffmpeg", "-y", "-f", "lavfi",
		"-i", "testsrc=size=64x48:d=2", "-pix_fmt", "yuv420p", videoPath).CombinedOutput()
	if err != nil {
		t.Fatalf("ffmpeg 生成测试视频失败: %v\n%s", err, string(out))
	}

	lib := &repo.Library{Name: "视频库", Path: dir, Kind: "video"}
	if err := lr.Create(lib); err != nil {
		t.Fatal(err)
	}

	svc := &Service{Sessions: sr, Media: mr, VideoThumbBase: t.TempDir()}
	stats, err := svc.ScanLibrary(context.Background(), *lib, "video-scan-1", false)
	if err != nil {
		t.Fatalf("ScanLibrary: %v", err)
	}
	if stats.Found != 1 || stats.Imported != 1 || stats.Skipped != 0 {
		t.Fatalf("第一次扫描统计错误: %+v", stats)
	}

	m, err := mr.GetByPath(lib.ID, "sample.mp4")
	if err != nil || m == nil {
		t.Fatalf("GetByPath: %+v %v", m, err)
	}
	if m.Sha1 != nil {
		t.Fatalf("主扫描不应生成视频 SHA1: %q", *m.Sha1)
	}
	if m.Oshash == nil || m.Phash == nil || m.DurationMs == nil || m.Width == nil || m.Height == nil {
		t.Fatalf("视频 metadata 缺失: %+v", m)
	}
	if m.ThumbnailPath == nil || *m.ThumbnailPath == "" {
		t.Fatalf("视频封面未生成: %+v", m)
	}
	if _, err := os.Stat(filepath.Join(svc.VideoThumbBase, filepath.FromSlash(*m.ThumbnailPath))); err != nil {
		t.Fatalf("封面文件不存在: %v", err)
	}

	// 视频无 sha1 时第二次扫描仍应增量跳过（needsSync 不把 sha1 当作视频完整性要求）
	stats, err = svc.ScanLibrary(context.Background(), *lib, "video-scan-2", false)
	if err != nil {
		t.Fatalf("第二次扫描: %v", err)
	}
	if stats.Imported != 0 || stats.Skipped != 1 {
		t.Fatalf("视频增量跳过统计错误: %+v", stats)
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
	progress := repo.ProgressFunc(func(string, int, int, int, int, int, int64, int64, float64, *int64) {})

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

// TestExecuteScanProgressBytesExcludeSkipped 验证字节统计只累计实际工作量，跳过文件不计入。
func TestExecuteScanProgressBytesExcludeSkipped(t *testing.T) {
	dbh := newTestDB(t)
	lr := repo.NewLibraryRepo(dbh)
	mr := repo.NewMediaRepo(dbh)
	sr := repo.NewSessionRepo(dbh)

	dir := t.TempDir()
	thumbDir := t.TempDir()
	writePNG(t, filepath.Join(dir, "a.png"), 10, 7)
	writePNG(t, filepath.Join(dir, "b.png"), 12, 7)
	lib := &repo.Library{Name: "字节库", Path: dir, Kind: "image"}
	if err := lr.Create(lib); err != nil {
		t.Fatal(err)
	}
	svc := &Service{Sessions: sr, Media: mr, ImageThumbBase: thumbDir}

	var lastTotalBytes, lastDoneBytes int64
	progress := repo.ProgressFunc(func(phase string, total, processed, succeeded, skipped, failed int, totalBytes, processedBytes int64, rate float64, eta *int64) {
		if phase == "processing" {
			lastTotalBytes, lastDoneBytes = totalBytes, processedBytes
		}
	})

	result, err := svc.ExecuteScan(context.Background(), *lib, "bytes-1", false, false, 2, progress)
	if err != nil {
		t.Fatal(err)
	}
	infoA, err := os.Stat(filepath.Join(dir, "a.png"))
	if err != nil {
		t.Fatal(err)
	}
	infoB, err := os.Stat(filepath.Join(dir, "b.png"))
	if err != nil {
		t.Fatal(err)
	}
	want := infoA.Size() + infoB.Size()
	if result.TotalBytes != want || result.ProcessedBytes != want {
		t.Fatalf("字节统计错误: total=%d processed=%d want=%d", result.TotalBytes, result.ProcessedBytes, want)
	}
	if lastTotalBytes != want || lastDoneBytes != want {
		t.Fatalf("进度字节错误: total=%d done=%d want=%d", lastTotalBytes, lastDoneBytes, want)
	}

	// 第二次扫描全部跳过：字节统计应为 0（跳过不计入工作量）
	result, err = svc.ExecuteScan(context.Background(), *lib, "bytes-2", false, false, 2, progress)
	if err != nil {
		t.Fatal(err)
	}
	if result.Skipped != 2 || result.TotalBytes != 0 || result.ProcessedBytes != 0 {
		t.Fatalf("跳过文件不应计入字节: %+v", result)
	}
	if lastTotalBytes != 0 || lastDoneBytes != 0 {
		t.Fatalf("进度字节未随跳过清零: total=%d done=%d", lastTotalBytes, lastDoneBytes)
	}
}

// TestMediaSnapshotDecisions 验证内存快照的增量/完整性判定与逐文件查询语义一致。
func TestMediaSnapshotDecisions(t *testing.T) {
	dbh := newTestDB(t)
	lr := repo.NewLibraryRepo(dbh)
	mr := repo.NewMediaRepo(dbh)

	dir := t.TempDir()
	lib := &repo.Library{Name: "快照库", Path: dir, Kind: "image"}
	if err := lr.Create(lib); err != nil {
		t.Fatal(err)
	}
	mt := time.Now()
	ph := "0123456789abcdef"
	w, h := 10, 10
	thumb := "ab/key.jpg"
	thumbLegacy := "ab/legacy.png"
	format := "png"
	if err := mr.Upsert(&repo.Media{
		LibraryID: lib.ID, Kind: "image", RelativePath: "ok.png",
		FileSize: 100, Mtime: mt, Format: &format, Width: &w, Height: &h,
		Phash: &ph, ThumbnailPath: &thumb,
	}); err != nil {
		t.Fatal(err)
	}
	if err := mr.Upsert(&repo.Media{
		LibraryID: lib.ID, Kind: "image", RelativePath: "oldformat.png",
		FileSize: 150, Mtime: mt, Format: &format, Width: &w, Height: &h,
		Phash: &ph, ThumbnailPath: &thumbLegacy,
	}); err != nil {
		t.Fatal(err)
	}
	if err := mr.Upsert(&repo.Media{
		LibraryID: lib.ID, Kind: "image", RelativePath: "broken.png",
		FileSize: 200, Mtime: mt, Format: &format, Width: &w, Height: &h,
	}); err != nil {
		t.Fatal(err)
	}

	svc := &Service{Media: mr}
	snap, err := svc.loadMediaSnapshot(lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.byPath) != 3 {
		t.Fatalf("快照记录数错误: %d", len(snap.byPath))
	}
	okEntry := media.FileEntry{RelativePath: "ok.png", Size: 100, Mtime: mt}
	if svc.needScanFromSnapshot(snap, okEntry) {
		t.Fatal("属性未变化应跳过")
	}
	changed := okEntry
	changed.Size = 101
	if !svc.needScanFromSnapshot(snap, changed) {
		t.Fatal("size 变化应重扫")
	}
	if !svc.needScanFromSnapshot(snap, media.FileEntry{RelativePath: "new.png"}) {
		t.Fatal("新文件应重扫")
	}
	if svc.needRepairFromSnapshot(snap, okEntry) {
		t.Fatal("字段完整应跳过修补")
	}
	if !svc.needRepairFromSnapshot(snap, media.FileEntry{RelativePath: "oldformat.png", Kind: media.KindImage}) {
		t.Fatal("旧格式 .png 缩略图应修补重生")
	}
	if !svc.needRepairFromSnapshot(snap, media.FileEntry{RelativePath: "broken.png", Kind: media.KindImage}) {
		t.Fatal("缺失 phash/缩略图应修补")
	}
	if !svc.needRepairFromSnapshot(snap, media.FileEntry{RelativePath: "missing.png", Kind: media.KindImage}) {
		t.Fatal("无记录应修补")
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
	progress := repo.ProgressFunc(func(string, int, int, int, int, int, int64, int64, float64, *int64) {})

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
