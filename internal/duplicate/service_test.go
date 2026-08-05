// service_test.go：重复报告服务端到端集成测试（扫描 → 生成 → 分页/树 → 一键清除）。
// 代码注释使用中文。
package duplicate

import (
	"bytes"
	"context"
	"encoding/json"
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
	"memable/internal/scan"
)

func writePNG(t *testing.T, path string, c color.RGBA, size int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestVideoSHA1ExactGrouping 验证视频补齐 sha1 后参与 sha1_exact 精确去重，缺失时不生成该分组。
func TestVideoSHA1ExactGrouping(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{Path: ":memory:"},
		Similarity: config.SimilarityConfig{
			VideoPHashDistance: 12, VideoDurationDiffMs: 3000,
		},
	}
	dbh, err := db.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer dbh.Close()
	if err := db.Migrate(dbh); err != nil {
		t.Fatal(err)
	}

	lr := repo.NewLibraryRepo(dbh)
	mr := repo.NewMediaRepo(dbh)
	lib := &repo.Library{Name: "视频库", Path: t.TempDir(), Kind: "video"}
	if err := lr.Create(lib); err != nil {
		t.Fatal(err)
	}

	// 两个视频 sha1 相同、sprite pHash 不同（差异 > 阈值），用于隔离 sha1_exact 分组
	mt := time.Now()
	sha := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	ph1 := "0123456789abcdef"
	ph2 := "fedcba9876543210"
	rel1, rel2 := "videos/a.mp4", "videos/b.mp4"
	dur := int64(1000)
	osh := "oshash"
	w, h := 640, 480
	format := "mp4"
	for _, m := range []*repo.Media{
		{LibraryID: lib.ID, Kind: "video", RelativePath: rel1, FileSize: 100, Mtime: mt,
			Format: &format, Width: &w, Height: &h, Phash: &ph1, DurationMs: &dur, Oshash: &osh, Sha1: &sha},
		{LibraryID: lib.ID, Kind: "video", RelativePath: rel2, FileSize: 100, Mtime: mt,
			Format: &format, Width: &w, Height: &h, Phash: &ph2, DurationMs: &dur, Oshash: &osh, Sha1: &sha},
	} {
		if err := mr.Upsert(m); err != nil {
			t.Fatal(err)
		}
	}

	det := NewDetector(mr, cfg)
	groups, err := det.DetectWithOptions(Options{MediaType: "video", IncludeSHA1: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || groups[0].Reason != "sha1_exact" || len(groups[0].Media) != 2 {
		t.Fatalf("补齐 sha1 后应生成 sha1_exact 分组: %+v", groups)
	}

	// 清空 sha1 后仍按短视频 OSHash 相同生成相似组；pHash 差异不参与判断。
	if _, err := dbh.Exec(`UPDATE media SET sha1 = NULL`); err != nil {
		t.Fatal(err)
	}
	groups2, err := det.DetectWithOptions(Options{MediaType: "video", IncludeSHA1: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(groups2) != 1 || groups2[0].Reason != "oshash_short_exact" || len(groups2[0].Media) != 2 {
		t.Fatalf("无 sha1 时应按相同 oshash 生成短视频相似组: %+v", groups2)
	}
}

func TestDuplicateServiceEndToEnd(t *testing.T) {
	root := t.TempDir()
	libDir := filepath.Join(root, "lib")
	thumbDir := filepath.Join(root, "thumbs")
	if err := os.MkdirAll(filepath.Join(libDir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	// a/b 完全相同；d 与 a 字节相同但在子目录；c 为不同颜色的图
	writePNG(t, filepath.Join(libDir, "a.png"), color.RGBA{R: 220, G: 40, B: 40, A: 255}, 64)
	writePNG(t, filepath.Join(libDir, "b.png"), color.RGBA{R: 220, G: 40, B: 40, A: 255}, 64)
	writePNG(t, filepath.Join(libDir, "c.png"), color.RGBA{R: 40, G: 40, B: 220, A: 255}, 64)
	writePNG(t, filepath.Join(libDir, "sub", "d.png"), color.RGBA{R: 220, G: 40, B: 40, A: 255}, 64)
	// 深层子目录内再放一对完全相同文件，验证嵌套目录树
	deepDir := filepath.Join(libDir, "sub", "deep")
	if err := os.MkdirAll(deepDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writePNG(t, filepath.Join(deepDir, "e.png"), color.RGBA{R: 20, G: 180, B: 20, A: 255}, 32)
	writePNG(t, filepath.Join(deepDir, "f.png"), color.RGBA{R: 20, G: 180, B: 20, A: 255}, 32)

	cfg := &config.Config{
		Database:  config.DatabaseConfig{Path: ":memory:"},
		Thumbnail: config.ThumbnailConfig{ImageDir: thumbDir, VideoDir: thumbDir, MaxEdge: 300},
		Video:     config.VideoConfig{SpriteFrames: 25},
		Similarity: config.SimilarityConfig{
			ImagePHashDistance: 10, VideoPHashDistance: 12, VideoDurationDiffMs: 3000,
		},
		Worker: config.WorkerConfig{PoolSize: 2},
	}
	dbh, err := db.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer dbh.Close()
	if err := db.Migrate(dbh); err != nil {
		t.Fatal(err)
	}

	libRepo := repo.NewLibraryRepo(dbh)
	sessionRepo := repo.NewSessionRepo(dbh)
	mediaRepo := repo.NewMediaRepo(dbh)
	dupRepo := repo.NewDuplicateRepo(dbh)

	lib := &repo.Library{Name: "测试库", Path: libDir, Kind: "mixed"}
	if err := libRepo.Create(lib); err != nil {
		t.Fatal(err)
	}

	// 同步扫描
	scanSvc := &scan.Service{
		Sessions: sessionRepo, Media: mediaRepo, Config: cfg,
		ImageThumbBase: thumbDir, Libraries: libRepo,
	}
	noop := func(string, int, int, int, int, int, int64, int64, float64, *int64) {}
	result, err := scanSvc.ExecuteScan(context.Background(), *lib, "session-1", false, false, 2, noop)
	if err != nil {
		t.Fatal(err)
	}
	if result.Imported != 6 {
		t.Fatalf("应导入 6 个文件，实际 %d", result.Imported)
	}

	svc := NewService(dupRepo, repo.NewDirDuplicateRepo(dbh), mediaRepo, libRepo, cfg, thumbDir, thumbDir)

	// 全部数据：SHA1 组应包含 a/b/d（3 个文件）
	repAll, err := svc.Generate(Options{
		Scope: "all", MediaType: "all", ImageThreshold: 90,
		VideoPhashDistance: 12, VideoDurationDiffMs: 3000,
		OshashFilter: true, IncludeSHA1: true,
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	if repAll.TotalGroups != 2 || repAll.TotalFiles != 5 {
		t.Fatalf("全部模式应 2 组 5 文件，实际 %d 组 %d 文件", repAll.TotalGroups, repAll.TotalFiles)
	}
	allTree, err := svc.Tree("")
	if err != nil {
		t.Fatal(err)
	}
	// 全部模式下，a/b/d 跨目录重复：根目录应计 2 个，sub 应计 1 个；
	// sub/deep 的同目录组仍计 2 个。
	if len(allTree) != 2 || allTree[0].Path != "" || allTree[0].FileCount != 2 {
		t.Fatalf("全部模式根目录树异常: %+v", allTree)
	}
	if allTree[1].Path != "sub" || allTree[1].FileCount != 1 ||
		len(allTree[1].Children) != 1 || allTree[1].Children[0].FileCount != 2 {
		t.Fatalf("全部模式跨目录重复树异常: %+v", allTree)
	}

	// 目录树 kind 过滤回归：全库只有图片，image 过滤结果与全量一致，video 过滤应为空，
	// 否则前端按类型过滤后点击其它类型独占的目录节点会无数据。
	imageTree, err := svc.Tree("image")
	if err != nil {
		t.Fatal(err)
	}
	if len(imageTree) != 2 || imageTree[0].Path != "" || imageTree[0].FileCount != 2 {
		t.Fatalf("image 过滤目录树异常: %+v", imageTree)
	}
	videoTree, err := svc.Tree("video")
	if err != nil {
		t.Fatal(err)
	}
	if len(videoTree) != 0 {
		t.Fatalf("video 过滤应返回空树，实际 %+v", videoTree)
	}

	// 仅同一目录：根目录 a/b 成组；sub/deep 下 e/f 成组；sub 的 d 不成组
	repSame, err := svc.Generate(Options{
		Scope: "same_dir", MediaType: "all", ImageThreshold: 90,
		VideoPhashDistance: 12, VideoDurationDiffMs: 3000,
		OshashFilter: true, IncludeSHA1: true,
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	if repSame.TotalGroups != 2 || repSame.TotalFiles != 4 {
		t.Fatalf("同目录模式应 2 组 4 文件，实际 %d 组 %d 文件", repSame.TotalGroups, repSame.TotalFiles)
	}

	// 分组分页与目录树
	page, err := svc.Groups(1, 20, "all", "")
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || len(page.Items) != 2 {
		t.Fatalf("分组分页数据异常: %+v", page)
	}
	tree, err := svc.Tree("")
	if err != nil {
		t.Fatal(err)
	}
	// 树应包含根目录节点（2 个文件）与嵌套目录 sub/deep（2 个文件）
	if len(tree) != 2 {
		t.Fatalf("目录树应返回 2 个根节点，实际 %+v", tree)
	}
	rootNode := tree[0]
	if rootNode.Path != "" || rootNode.FileCount != 2 {
		t.Fatalf("根节点异常: %+v", rootNode)
	}
	subNode := tree[1]
	if subNode.Path != "sub" || len(subNode.Children) != 1 ||
		subNode.Children[0].Path != "sub/deep" || subNode.Children[0].FileCount != 2 {
		t.Fatalf("嵌套目录节点异常: %+v", subNode)
	}

	// 一键清除本页（保留最小文件）：删除组内其余 1 个文件，组消失
	groupIDs := make([]int64, 0, len(page.Items))
	for _, g := range page.Items {
		groupIDs = append(groupIDs, g.ID)
	}
	clearResult, err := svc.Clear(ClearRequest{
		Scope: "page", Keep: "smallest", GroupIDs: groupIDs, Permanent: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if clearResult.DeletedFiles != 2 {
		t.Fatalf("应删除 2 个文件（每组保留 1 个），实际 %d", clearResult.DeletedFiles)
	}
	summary, err := svc.Summary()
	if err != nil {
		t.Fatal(err)
	}
	if summary.Report.TotalGroups != 0 {
		t.Fatalf("清除后应无重复组，实际 %d 组", summary.Report.TotalGroups)
	}
	remainingPage, err := svc.Groups(1, 20, "all", "")
	if err != nil {
		t.Fatal(err)
	}
	if remainingPage.Total != 0 || len(remainingPage.Items) != 0 {
		t.Fatalf("清除后不应返回单成员重复组: %+v", remainingPage)
	}
	remainingTree, err := svc.Tree("")
	if err != nil {
		t.Fatal(err)
	}
	if len(remainingTree) != 0 {
		t.Fatalf("清除后目录树不应保留重复目录: %+v", remainingTree)
	}
	if _, err := os.Stat(filepath.Join(libDir, "b.png")); !os.IsNotExist(err) {
		t.Fatalf("b.png 应已删除")
	}
	if _, err := os.Stat(filepath.Join(libDir, "a.png")); err != nil {
		t.Fatalf("保留文件 a.png 应存在: %v", err)
	}
	if _, err := os.Stat(filepath.Join(libDir, "sub", "deep", "f.png")); !os.IsNotExist(err) {
		t.Fatalf("f.png 应已删除")
	}
	if _, err := os.Stat(filepath.Join(libDir, "sub", "deep", "e.png")); err != nil {
		t.Fatalf("保留文件 e.png 应存在: %v", err)
	}
}

// TestClearDirectoryScope 回归：目录树"删除此目录下所有重复数据"只删除本目录数据——
// 组内本目录成员 >=2 按保留条件保留 1 个；组内本目录成员 ==1（仅跨目录重复）直接删除；
// 其它目录成员一律不动。
func TestClearDirectoryScope(t *testing.T) {
	root := t.TempDir()
	libDir := filepath.Join(root, "lib")
	thumbDir := filepath.Join(root, "thumbs")
	for _, d := range []string{"sub/deep", "other"} {
		if err := os.MkdirAll(filepath.Join(libDir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// 组 A：a/b 在根目录 + d 在 sub（跨目录）；组 B：e/f 在 sub/deep；组 C：g/h 在 other
	writePNG(t, filepath.Join(libDir, "a.png"), color.RGBA{R: 220, G: 40, B: 40, A: 255}, 64)
	writePNG(t, filepath.Join(libDir, "b.png"), color.RGBA{R: 220, G: 40, B: 40, A: 255}, 64)
	writePNG(t, filepath.Join(libDir, "sub", "d.png"), color.RGBA{R: 220, G: 40, B: 40, A: 255}, 64)
	writePNG(t, filepath.Join(libDir, "sub", "deep", "e.png"), color.RGBA{R: 20, G: 180, B: 20, A: 255}, 32)
	writePNG(t, filepath.Join(libDir, "sub", "deep", "f.png"), color.RGBA{R: 20, G: 180, B: 20, A: 255}, 32)
	writePNG(t, filepath.Join(libDir, "other", "g.png"), color.RGBA{R: 100, G: 100, B: 200, A: 255}, 32)
	writePNG(t, filepath.Join(libDir, "other", "h.png"), color.RGBA{R: 100, G: 100, B: 200, A: 255}, 32)

	cfg := &config.Config{
		Database:  config.DatabaseConfig{Path: ":memory:"},
		Thumbnail: config.ThumbnailConfig{ImageDir: thumbDir, VideoDir: thumbDir, MaxEdge: 300},
		Video:     config.VideoConfig{SpriteFrames: 25},
		Similarity: config.SimilarityConfig{
			ImagePHashDistance: 10, VideoPHashDistance: 12, VideoDurationDiffMs: 3000,
		},
		Worker: config.WorkerConfig{PoolSize: 2},
	}
	dbh, err := db.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer dbh.Close()
	if err := db.Migrate(dbh); err != nil {
		t.Fatal(err)
	}

	libRepo := repo.NewLibraryRepo(dbh)
	sessionRepo := repo.NewSessionRepo(dbh)
	mediaRepo := repo.NewMediaRepo(dbh)
	dupRepo := repo.NewDuplicateRepo(dbh)

	lib := &repo.Library{Name: "测试库", Path: libDir, Kind: "mixed"}
	if err := libRepo.Create(lib); err != nil {
		t.Fatal(err)
	}
	scanSvc := &scan.Service{
		Sessions: sessionRepo, Media: mediaRepo, Config: cfg,
		ImageThumbBase: thumbDir, Libraries: libRepo,
	}
	noop := func(string, int, int, int, int, int, int64, int64, float64, *int64) {}
	if _, err := scanSvc.ExecuteScan(context.Background(), *lib, "session-1", false, false, 2, noop); err != nil {
		t.Fatal(err)
	}
	svc := NewService(dupRepo, repo.NewDirDuplicateRepo(dbh), mediaRepo, libRepo, cfg, thumbDir, thumbDir)
	if _, err := svc.Generate(Options{
		Scope: "all", MediaType: "all", ImageThreshold: 90,
		VideoPhashDistance: 12, VideoDurationDiffMs: 3000,
		OshashFilter: true, IncludeSHA1: true,
	}, ""); err != nil {
		t.Fatal(err)
	}

	exist := func(rel string) bool {
		_, err := os.Stat(filepath.Join(libDir, filepath.FromSlash(rel)))
		return err == nil
	}

	// 场景 1：清除根目录。组 A 在根目录有 a/b（>=2），保留 1 个删 1 个；
	// sub/d 与 other 组均不受影响。
	res, err := svc.Clear(ClearRequest{
		Scope: "directory", Keep: "largest", Directory: "", Permanent: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.DeletedFiles != 1 {
		t.Fatalf("清除根目录应删 1 个文件，实际 %d", res.DeletedFiles)
	}
	if exist("sub/d.png") == false || exist("other/g.png") == false ||
		exist("sub/deep/e.png") == false || exist("sub/deep/f.png") == false {
		t.Fatalf("清除根目录不应影响其它目录文件")
	}
	rootRemain := exist("a.png") || exist("b.png")
	if !rootRemain {
		t.Fatalf("根目录应保留 1 个文件（a 或 b）")
	}

	// 场景 2：清除 sub 目录。组 A 在 sub 只有 d 一个成员（仅跨目录重复），直接删除；
	// sub/deep 的 e/f 属于 sub/deep 目录，不受影响。
	res, err = svc.Clear(ClearRequest{
		Scope: "directory", Keep: "largest", Directory: "sub", Permanent: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.DeletedFiles != 1 {
		t.Fatalf("清除 sub 应删 1 个文件（d），实际 %d", res.DeletedFiles)
	}
	if exist("sub/d.png") {
		t.Fatalf("sub/d.png 应已删除")
	}
	if exist("sub/deep/e.png") == false || exist("sub/deep/f.png") == false ||
		exist("other/g.png") == false || exist("other/h.png") == false {
		t.Fatalf("清除 sub 不应影响其它目录文件")
	}

	// 场景 3：清除 sub/deep。组 B 在 sub/deep 有 e/f（>=2），保留 1 个删 1 个。
	res, err = svc.Clear(ClearRequest{
		Scope: "directory", Keep: "largest", Directory: "sub/deep", Permanent: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.DeletedFiles != 1 {
		t.Fatalf("清除 sub/deep 应删 1 个文件，实际 %d", res.DeletedFiles)
	}
	deepRemain := exist("sub/deep/e.png") || exist("sub/deep/f.png")
	if !deepRemain {
		t.Fatalf("sub/deep 应保留 1 个文件（e 或 f）")
	}
	if exist("other/g.png") == false || exist("other/h.png") == false {
		t.Fatalf("清除 sub/deep 不应影响其它目录文件")
	}

	// 场景 4：清除 other。组 C 在 other 有 g/h（>=2），保留 1 个删 1 个。
	res, err = svc.Clear(ClearRequest{
		Scope: "directory", Keep: "largest", Directory: "other", Permanent: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.DeletedFiles != 1 {
		t.Fatalf("清除 other 应删 1 个文件，实际 %d", res.DeletedFiles)
	}
	otherRemain := exist("other/g.png") || exist("other/h.png")
	if !otherRemain {
		t.Fatalf("other 应保留 1 个文件（g 或 h）")
	}
}

// TestBuildDirTreeNested 回归：深层嵌套目录构建目录树时不得无限递归（栈溢出）。
func TestBuildDirTreeNested(t *testing.T) {
	roots := buildDirTree(map[string]int{
		".":     2,
		"a":     1,
		"a/b":   3,
		"a/b/c": 4,
		"x":     5,
	})
	if len(roots) != 3 {
		t.Fatalf("应返回 3 个根节点（根目录/a/x），实际 %d", len(roots))
	}
	if roots[0].Path != "" || roots[0].FileCount != 2 {
		t.Fatalf("根目录节点应为 path=\"\" file_count=2，实际 %+v", roots[0])
	}
	var a *TreeItem
	for _, r := range roots {
		if r.Path == "a" {
			a = r
		}
	}
	if a == nil || len(a.Children) != 1 || a.Children[0].Path != "a/b" {
		t.Fatalf("节点 a 应包含子节点 a/b，实际 %+v", a)
	}
	b := a.Children[0]
	if len(b.Children) != 1 || b.Children[0].Path != "a/b/c" || b.Children[0].FileCount != 4 {
		t.Fatalf("节点 a/b 的子节点 c 数据异常: %+v", b.Children)
	}
}

// TestBuildDirTreeKeepsEmptyIntermediateNodes 回归：只有最深层目录包含重复文件时，
// 中间目录也必须完整挂接，否则前端只能看到没有子节点的顶层目录。
func TestBuildDirTreeKeepsEmptyIntermediateNodes(t *testing.T) {
	roots := buildDirTree(map[string]int{
		"library/album/photos": 4,
		"library/video/covers": 2,
	})
	if len(roots) != 1 || roots[0].Path != "library" {
		t.Fatalf("应仅有 library 根节点，实际 %+v", roots)
	}
	library := roots[0]
	if library.FileCount != 0 || len(library.Children) != 2 {
		t.Fatalf("library 应包含两个中间目录且无直属文件，实际 %+v", library)
	}
	album := library.Children[0]
	if album.Path != "library/album" || album.FileCount != 0 || len(album.Children) != 1 {
		t.Fatalf("album 中间节点异常: %+v", album)
	}
	photos := album.Children[0]
	if photos.Path != "library/album/photos" || photos.FileCount != 4 {
		t.Fatalf("photos 叶子节点异常: %+v", photos)
	}
	video := library.Children[1]
	if video.Path != "library/video" || video.FileCount != 0 || len(video.Children) != 1 {
		t.Fatalf("video 中间节点异常: %+v", video)
	}
	if covers := video.Children[0]; covers.Path != "library/video/covers" || covers.FileCount != 2 {
		t.Fatalf("covers 叶子节点异常: %+v", covers)
	}
}

// TestReportJSONFieldNames 回归：报告/成员/清除请求的 JSON 字段名必须与客户端一致（snake_case）。
func TestReportJSONFieldNames(t *testing.T) {
	rep := &repo.DuplicateReport{
		ID: 1, Scope: "same_dir", MediaType: "all", ImageThreshold: 90,
		VideoPhashDistance: 12, VideoDurationDiffMs: 3000,
		OshashFilter: true, IncludeSHA1: true,
		TotalGroups: 2, TotalFiles: 4,
	}
	b, err := json.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m["scope"] != "same_dir" || m["media_type"] != "all" || m["total_groups"] != float64(2) {
		t.Fatalf("报告 JSON 字段名不符: %v", m)
	}
	if m["oshash_filter"] != true || m["include_sha1"] != true {
		t.Fatalf("报告 JSON 布尔字段不符: %v", m)
	}

	// 成员视图：full_path / library_name 必须可被客户端读取
	mv := repo.MediaView{
		Media:       repo.Media{ID: 9, Kind: "image", RelativePath: "a.jpg", FileSize: 100},
		FullPath:    "D:/Pictures/a.jpg",
		LibraryName: "图库",
	}
	b2, _ := json.Marshal(mv)
	var mvMap map[string]any
	if err := json.Unmarshal(b2, &mvMap); err != nil {
		t.Fatal(err)
	}
	if mvMap["full_path"] != "D:/Pictures/a.jpg" || mvMap["library_name"] != "图库" || mvMap["id"] != float64(9) {
		t.Fatalf("成员视图 JSON 字段名不符: %v", mvMap)
	}

	// 一键清除请求：group_ids 必须能反序列化
	reqJSON := `{"scope":"page","keep":"largest","group_ids":[1,2],"permanent":true}`
	var req ClearRequest
	if err := json.Unmarshal([]byte(reqJSON), &req); err != nil {
		t.Fatal(err)
	}
	if req.Scope != "page" || req.Keep != "largest" || len(req.GroupIDs) != 2 || !req.Permanent {
		t.Fatalf("清除请求反序列化不符: %+v", req)
	}
}

// TestExcludeMediaRemovesFromCurrentReport 验证"排除重复"：
// 移除文件在当前报告中的全部成员关系，成员数 <2 的组被清理、统计刷新；
// 文件不在报告中时幂等返回 0；重新生成报告后文件重新参与检测。
func TestExcludeMediaRemovesFromCurrentReport(t *testing.T) {
	cfg := &config.Config{Database: config.DatabaseConfig{Path: ":memory:"}}
	dbh, err := db.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer dbh.Close()
	if err := db.Migrate(dbh); err != nil {
		t.Fatal(err)
	}

	lr := repo.NewLibraryRepo(dbh)
	mr := repo.NewMediaRepo(dbh)
	lib := &repo.Library{Name: "图库", Path: t.TempDir(), Kind: "image"}
	if err := lr.Create(lib); err != nil {
		t.Fatal(err)
	}

	// a/b 完全相同（sha1 相同 → sha1_exact 组），c 无重复
	mt := time.Now()
	sha := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	ph := "0123456789abcdef"
	format := "png"
	var idA, idB, idC int64
	for _, m := range []*repo.Media{
		{LibraryID: lib.ID, Kind: "image", RelativePath: "a.png", FileSize: 100, Mtime: mt, Sha1: &sha, Phash: &ph, Format: &format},
		{LibraryID: lib.ID, Kind: "image", RelativePath: "b.png", FileSize: 100, Mtime: mt, Sha1: &sha, Phash: &ph, Format: &format},
		{LibraryID: lib.ID, Kind: "image", RelativePath: "c.png", FileSize: 100, Mtime: mt, Phash: &ph, Format: &format},
	} {
		if err := mr.Upsert(m); err != nil {
			t.Fatal(err)
		}
		if m.RelativePath == "a.png" {
			idA = m.ID
		} else if m.RelativePath == "b.png" {
			idB = m.ID
		} else {
			idC = m.ID
		}
	}

	svc := NewService(repo.NewDuplicateRepo(dbh), repo.NewDirDuplicateRepo(dbh), mr, lr, cfg, "", "")
	rep, err := svc.Generate(Options{Scope: "all", MediaType: "image", ImageThreshold: 90, IncludeSHA1: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	if rep.TotalGroups != 1 || rep.TotalFiles != 2 {
		t.Fatalf("报告应含 1 组 2 文件，实际 %+v", rep)
	}

	// 排除 a：移除成员、组被清理、统计归零
	removed, err := svc.ExcludeMedia(idA)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("应移除 1 个成员，实际 %d", removed)
	}
	page, err := svc.Groups(1, 20, "all", "")
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 0 || len(page.Items) != 0 {
		t.Fatalf("排除后报告应为空: %+v", page)
	}
	latest, err := repo.NewDuplicateRepo(dbh).GetLatestReport()
	if err != nil || latest == nil {
		t.Fatalf("查询最新报告失败: %v", err)
	}
	if latest.TotalGroups != 0 || latest.TotalFiles != 0 {
		t.Fatalf("报告统计未刷新: %+v", latest)
	}

	// 幂等：再次排除同一文件返回 0
	removed, err = svc.ExcludeMedia(idA)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 0 {
		t.Fatalf("重复排除应返回 0，实际 %d", removed)
	}
	// 排除不在报告中的文件（c）返回 0
	if removed, err = svc.ExcludeMedia(idC); err != nil {
		t.Fatal(err)
	}
	if removed != 0 {
		t.Fatalf("排除报告外文件应返回 0，实际 %d", removed)
	}

	// 重新生成报告后，被排除的文件重新参与检测
	rep2, err := svc.Generate(Options{Scope: "all", MediaType: "image", ImageThreshold: 90, IncludeSHA1: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	if rep2.TotalGroups != 1 || rep2.TotalFiles != 2 {
		t.Fatalf("重新生成后 a/b 应重新成组: %+v", rep2)
	}
	page2, err := svc.Groups(1, 20, "all", "")
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]int64, 0, 2)
	for _, it := range page2.Items[0].Items {
		ids = append(ids, it.ID)
	}
	if len(ids) != 2 || (ids[0] != idA && ids[1] != idA) || (ids[0] != idB && ids[1] != idB) {
		t.Fatalf("重新生成的组应包含 a/b，实际 %v", ids)
	}
}

// TestGenerateIncrementalAndFullRebuild 验证报告生成的增量检测与全量重建：
// 参数不变时新增媒体只补增量；阈值变化时全量重建。
func TestGenerateIncrementalAndFullRebuild(t *testing.T) {
	cfg := &config.Config{Database: config.DatabaseConfig{Path: ":memory:"}}
	dbh, err := db.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer dbh.Close()
	if err := db.Migrate(dbh); err != nil {
		t.Fatal(err)
	}

	lr := repo.NewLibraryRepo(dbh)
	mr := repo.NewMediaRepo(dbh)
	lib := &repo.Library{Name: "增量库", Path: t.TempDir(), Kind: "image"}
	if err := lr.Create(lib); err != nil {
		t.Fatal(err)
	}
	svc := NewService(repo.NewDuplicateRepo(dbh), repo.NewDirDuplicateRepo(dbh), mr, lr, cfg, "", "")

	// a/b sha1 相同成组
	mt := time.Now()
	shaA := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	shaB := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	for _, m := range []*repo.Media{
		{LibraryID: lib.ID, Kind: "image", RelativePath: "a.jpg", FileSize: 100, Mtime: mt, Sha1: &shaA},
		{LibraryID: lib.ID, Kind: "image", RelativePath: "b.jpg", FileSize: 100, Mtime: mt, Sha1: &shaA},
	} {
		if err := mr.Upsert(m); err != nil {
			t.Fatal(err)
		}
	}
	opts := Options{Scope: "all", MediaType: "image", ImageThreshold: 90, IncludeSHA1: true}
	rep1, err := svc.Generate(opts, "")
	if err != nil {
		t.Fatal(err)
	}
	if rep1.TotalGroups != 1 || rep1.TotalFiles != 2 {
		t.Fatalf("首次报告应 1 组 2 文件: %+v", rep1)
	}

	// 新增 c（与 a 同 sha1）、d（独立）：再次同参数生成，增量检测应纳入 c
	if err := mr.Upsert(&repo.Media{LibraryID: lib.ID, Kind: "image", RelativePath: "c.jpg", FileSize: 100, Mtime: mt, Sha1: &shaA}); err != nil {
		t.Fatal(err)
	}
	if err := mr.Upsert(&repo.Media{LibraryID: lib.ID, Kind: "image", RelativePath: "d.jpg", FileSize: 100, Mtime: mt, Sha1: &shaB}); err != nil {
		t.Fatal(err)
	}
	rep2, err := svc.Generate(opts, "")
	if err != nil {
		t.Fatal(err)
	}
	if rep2.TotalGroups != 1 || rep2.TotalFiles != 3 {
		t.Fatalf("增量后应 1 组 3 文件: %+v", rep2)
	}

	// 阈值变化：全量重建仍应得到正确结果（sha1 分组不受阈值影响）
	rep3, err := svc.Generate(Options{Scope: "all", MediaType: "image", ImageThreshold: 50, IncludeSHA1: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	if rep3.TotalGroups != 1 || rep3.TotalFiles != 3 {
		t.Fatalf("阈值变化全量重建应 1 组 3 文件: %+v", rep3)
	}
	if rep3.ImageThreshold != 50 {
		t.Fatalf("报告应记录新阈值: %+v", rep3)
	}
}

// TestGenerateIncrementalKeepsOldGroups 回归：无新增媒体时再次生成报告，
// 旧报告的重复分组必须保留（增量检测不得清空已有重复数据）。
func TestGenerateIncrementalKeepsOldGroups(t *testing.T) {
	cfg := &config.Config{Database: config.DatabaseConfig{Path: ":memory:"}}
	dbh, err := db.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer dbh.Close()
	if err := db.Migrate(dbh); err != nil {
		t.Fatal(err)
	}

	lr := repo.NewLibraryRepo(dbh)
	mr := repo.NewMediaRepo(dbh)
	lib := &repo.Library{Name: "保留库", Path: t.TempDir(), Kind: "image"}
	if err := lr.Create(lib); err != nil {
		t.Fatal(err)
	}
	svc := NewService(repo.NewDuplicateRepo(dbh), repo.NewDirDuplicateRepo(dbh), mr, lr, cfg, "", "")

	mt := time.Now()
	shaA := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	for _, m := range []*repo.Media{
		{LibraryID: lib.ID, Kind: "image", RelativePath: "a.jpg", FileSize: 100, Mtime: mt, Sha1: &shaA},
		{LibraryID: lib.ID, Kind: "image", RelativePath: "b.jpg", FileSize: 100, Mtime: mt, Sha1: &shaA},
	} {
		if err := mr.Upsert(m); err != nil {
			t.Fatal(err)
		}
	}
	opts := Options{Scope: "all", MediaType: "image", ImageThreshold: 90, IncludeSHA1: true}
	rep1, err := svc.Generate(opts, "")
	if err != nil {
		t.Fatal(err)
	}
	if rep1.TotalGroups != 1 || rep1.TotalFiles != 2 {
		t.Fatalf("首次报告应 1 组 2 文件: %+v", rep1)
	}

	// 数据无任何变化，再次生成：增量检测必须保留旧组
	rep2, err := svc.Generate(opts, "")
	if err != nil {
		t.Fatal(err)
	}
	if rep2.TotalGroups != 1 || rep2.TotalFiles != 2 {
		t.Fatalf("无变化再次生成应保留 1 组 2 文件: %+v", rep2)
	}
	page, err := svc.Groups(1, 20, "all", "")
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].MemberCount != 2 {
		t.Fatalf("分组数据应保留: %+v", page)
	}
}

// TestGenerateEmptyReportDoesNotPropagate 回归：旧报告为空（0 组）时，
// 再次生成必须走全量检测，禁止增量检测导致空报告传染。
func TestGenerateEmptyReportDoesNotPropagate(t *testing.T) {
	cfg := &config.Config{Database: config.DatabaseConfig{Path: ":memory:"}}
	dbh, err := db.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer dbh.Close()
	if err := db.Migrate(dbh); err != nil {
		t.Fatal(err)
	}

	lr := repo.NewLibraryRepo(dbh)
	mr := repo.NewMediaRepo(dbh)
	lib := &repo.Library{Name: "空报告库", Path: t.TempDir(), Kind: "image"}
	if err := lr.Create(lib); err != nil {
		t.Fatal(err)
	}
	svc := NewService(repo.NewDuplicateRepo(dbh), repo.NewDirDuplicateRepo(dbh), mr, lr, cfg, "", "")

	mt := time.Now()
	shaA := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	shaB := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	// a/b 相同成组，c 独立
	for _, m := range []*repo.Media{
		{LibraryID: lib.ID, Kind: "image", RelativePath: "a.jpg", FileSize: 100, Mtime: mt, Sha1: &shaA},
		{LibraryID: lib.ID, Kind: "image", RelativePath: "b.jpg", FileSize: 100, Mtime: mt, Sha1: &shaA},
		{LibraryID: lib.ID, Kind: "image", RelativePath: "c.jpg", FileSize: 100, Mtime: mt, Sha1: &shaB},
	} {
		if err := mr.Upsert(m); err != nil {
			t.Fatal(err)
		}
	}
	opts := Options{Scope: "all", MediaType: "image", ImageThreshold: 90, IncludeSHA1: true}
	rep1, err := svc.Generate(opts, "")
	if err != nil {
		t.Fatal(err)
	}
	if rep1.TotalGroups != 1 {
		t.Fatalf("首次报告应 1 组: %+v", rep1)
	}

	// 模拟历史空报告（如外键未启用时代的 bug 产物）：手工把当前报告改成空
	if _, err := dbh.Exec(`DELETE FROM duplicate_group_members`); err != nil {
		t.Fatal(err)
	}
	if _, err := dbh.Exec(`DELETE FROM duplicate_groups`); err != nil {
		t.Fatal(err)
	}
	if _, err := dbh.Exec(`UPDATE duplicate_reports SET total_groups=0, total_files=0`); err != nil {
		t.Fatal(err)
	}

	// 再次生成：旧报告为空 → 必须全量检测恢复分组
	rep2, err := svc.Generate(opts, "")
	if err != nil {
		t.Fatal(err)
	}
	if rep2.TotalGroups != 1 || rep2.TotalFiles != 2 {
		t.Fatalf("旧报告为空时应全量恢复 1 组 2 文件: %+v", rep2)
	}
}

// TestGenerateDirCompare 验证目录对比：所选目录（含子目录）与存量数据成组，
// 目标内部不比较；is_target 标记正确；独立三表存储不污染重复报告。
func TestGenerateDirCompare(t *testing.T) {
	cfg := &config.Config{Database: config.DatabaseConfig{Path: ":memory:"}}
	dbh, err := db.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer dbh.Close()
	if err := db.Migrate(dbh); err != nil {
		t.Fatal(err)
	}

	lr := repo.NewLibraryRepo(dbh)
	mr := repo.NewMediaRepo(dbh)
	svc := NewService(repo.NewDuplicateRepo(dbh), repo.NewDirDuplicateRepo(dbh), mr, lr, cfg, "", "")

	lib1 := &repo.Library{Name: "库1", Path: t.TempDir(), Kind: "image"}
	lib2 := &repo.Library{Name: "库2", Path: t.TempDir(), Kind: "image"}
	if err := lr.Create(lib1); err != nil {
		t.Fatal(err)
	}
	if err := lr.Create(lib2); err != nil {
		t.Fatal(err)
	}

	mt := time.Now()
	shaTarget := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	shaBoth := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	shaOther := "cccccccccccccccccccccccccccccccccccccccc"
	// 库1 target/ 目录：a1/a2 同 sha（目标内部相同，无存量匹配 → 不应成组）；a3 与存量 b 同 sha
	// 库1 other/ 目录：b（存量，与目标 a3 同 sha）
	// 库2 根目录：d（存量，与目标 a3 同 sha）
	for _, m := range []*repo.Media{
		{LibraryID: lib1.ID, Kind: "image", RelativePath: "target/a1.jpg", FileSize: 100, Mtime: mt, Sha1: &shaTarget},
		{LibraryID: lib1.ID, Kind: "image", RelativePath: "target/a2.jpg", FileSize: 100, Mtime: mt, Sha1: &shaTarget},
		{LibraryID: lib1.ID, Kind: "image", RelativePath: "target/sub/a3.jpg", FileSize: 100, Mtime: mt, Sha1: &shaBoth},
		{LibraryID: lib1.ID, Kind: "image", RelativePath: "other/b.jpg", FileSize: 100, Mtime: mt, Sha1: &shaBoth},
		{LibraryID: lib2.ID, Kind: "image", RelativePath: "d.jpg", FileSize: 100, Mtime: mt, Sha1: &shaBoth},
		{LibraryID: lib1.ID, Kind: "image", RelativePath: "other/e.jpg", FileSize: 100, Mtime: mt, Sha1: &shaOther},
	} {
		if err := mr.Upsert(m); err != nil {
			t.Fatal(err)
		}
	}

	opts := Options{MediaType: "image", ImageThreshold: 90, IncludeSHA1: true}
	rep, err := svc.GenerateDirCompare(opts, lib1.ID, "target", "")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Directory != "target" || rep.TotalGroups != 1 || rep.TotalFiles != 3 {
		t.Fatalf("目录对比报告应 1 组 3 文件: %+v", rep)
	}

	// 分组详情：组内 a3（目标）+ b、d（存量），is_target 标记正确
	views, err := svc.Dir.DirGroupViews(rep.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 1 || len(views[0].Items) != 3 {
		t.Fatalf("应只有 1 组 3 成员: %+v", views)
	}
	var targetCount, nonTargetCount int
	for _, it := range views[0].Items {
		if it.IsTarget {
			targetCount++
			if it.RelativePath != "target/sub/a3.jpg" {
				t.Fatalf("目标成员应为 a3: %+v", it)
			}
		} else {
			nonTargetCount++
		}
	}
	if targetCount != 1 || nonTargetCount != 2 {
		t.Fatalf("目标 1 存量 2，实际 目标 %d 存量 %d", targetCount, nonTargetCount)
	}

	// 目标内部相同（a1/a2 同 sha）无存量匹配，不应成组 → 已在 1 组断言中隐含验证

	// 独立存储：重复报告不受影响（无重复报告）
	dupRep, err := svc.Dup.GetLatestReport()
	if err != nil {
		t.Fatal(err)
	}
	if dupRep != nil {
		t.Fatalf("目录对比不应产生重复报告: %+v", dupRep)
	}

	// 分页接口（kind 过滤）
	page, err := svc.DirGroups(1, 20, "")
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].MemberCount != 3 {
		t.Fatalf("分页结果不符: %+v", page)
	}
	// kind=video 过滤后应为空
	pageVideo, err := svc.DirGroups(1, 20, "video")
	if err != nil {
		t.Fatal(err)
	}
	if pageVideo.Total != 0 {
		t.Fatalf("kind=video 过滤应无分组: %+v", pageVideo)
	}
	// 目录树：目录对比语义下所有含重复成员的目录都计入（即使目录内只有 1 个成员）。
	// 组内成员分布在 target/sub、other、根目录（d.jpg），对应树节点 target/sub、other 与根。
	tree, err := svc.DirTree("")
	if err != nil {
		t.Fatal(err)
	}
	var findNode func(nodes []*TreeItem, path string) *TreeItem
	findNode = func(nodes []*TreeItem, path string) *TreeItem {
		for _, n := range nodes {
			if n.Path == path {
				return n
			}
			if r := findNode(n.Children, path); r != nil {
				return r
			}
		}
		return nil
	}
	if n := findNode(tree, "target/sub"); n == nil || n.FileCount != 1 {
		t.Fatalf("目录树应含 target/sub(1): %+v", tree)
	}
	if n := findNode(tree, "other"); n == nil || n.FileCount != 1 {
		t.Fatalf("目录树应含 other(1): %+v", tree)
	}
	if n := findNode(tree, ""); n == nil || n.FileCount != 1 {
		t.Fatalf("目录树应含根节点(1): %+v", tree)
	}

	// 摘要
	summary, err := svc.DirSummary()
	if err != nil {
		t.Fatal(err)
	}
	if summary == nil || summary.Report == nil || summary.Report.Directory != "target" {
		t.Fatalf("摘要不符: %+v", summary)
	}

	// 排除重复：移除目标成员 a3，组剩 b/d（2 个）仍有效，统计刷新为 2 文件
	a3, err := mr.GetByPath(lib1.ID, "target/sub/a3.jpg")
	if err != nil {
		t.Fatal(err)
	}
	removed, err := svc.DirExcludeMedia(a3.ID)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("应移除 1 个成员，实际 %d", removed)
	}
	summary, err = svc.DirSummary()
	if err != nil {
		t.Fatal(err)
	}
	if summary.Report.TotalFiles != 2 || summary.Report.TotalGroups != 1 {
		t.Fatalf("排除后统计应 1 组 2 文件: %+v", summary.Report)
	}

	// 一键清除（page 范围）：组内 b/d 保留最大文件（大小相同保留首个），删除 1 个
	viewsAfter, err := svc.Dir.DirGroupViews(rep.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(viewsAfter) != 1 {
		t.Fatalf("排除后应 1 组: %+v", viewsAfter)
	}
	clearRes, err := svc.DirClear(ClearRequest{Scope: "page", Keep: "largest", GroupIDs: []int64{viewsAfter[0].ID}}, true)
	if err != nil {
		t.Fatal(err)
	}
	if clearRes.DeletedFiles != 1 {
		t.Fatalf("应删除 1 个文件: %+v", clearRes)
	}
	// 删除后组只剩 1 成员被 prune，报告无有效组
	page, err = svc.DirGroups(1, 20, "")
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 0 {
		t.Fatalf("清除后应无有效组: %+v", page)
	}

	// PruneAfterMediaChange 覆盖目录对比报告（不报错即通过）
	if err := svc.PruneAfterMediaChange(); err != nil {
		t.Fatalf("PruneAfterMediaChange: %v", err)
	}

	// 再次生成：替换旧目录对比报告（仍 1 份）
	rep2, err := svc.GenerateDirCompare(opts, lib1.ID, "target", "")
	if err != nil {
		t.Fatal(err)
	}
	if rep2.ID == rep.ID {
		t.Fatalf("重新生成应产生新报告: %d vs %d", rep2.ID, rep.ID)
	}
	latest, err := svc.Dir.GetLatestDirReport()
	if err != nil {
		t.Fatal(err)
	}
	if latest == nil || latest.ID != rep2.ID {
		t.Fatalf("最新目录对比报告不符: %+v", latest)
	}
}

// TestDirClearDirectoryScope 回归：目录对比"删除此目录下所有重复数据"与重复报告语义一致——
// 按"本目录成员"为单位处理、只删本目录数据：组内本目录成员 >=2 按保留条件保留 1 个删其余；
// 组内本目录成员 ==1（目标 vs 存量的典型形态）直接删除本目录这一份；其它目录成员一律不动。
func TestDirClearDirectoryScope(t *testing.T) {
	cfg := &config.Config{Database: config.DatabaseConfig{Path: ":memory:"}}
	dbh, err := db.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer dbh.Close()
	if err := db.Migrate(dbh); err != nil {
		t.Fatal(err)
	}

	lr := repo.NewLibraryRepo(dbh)
	mr := repo.NewMediaRepo(dbh)
	svc := NewService(repo.NewDuplicateRepo(dbh), repo.NewDirDuplicateRepo(dbh), mr, lr, cfg, "", "")

	root := t.TempDir()
	libDir := filepath.Join(root, "lib")
	for _, d := range []string{"target", "other"} {
		if err := os.MkdirAll(filepath.Join(libDir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// 组 A：target/a + other/b（目标 1 成员 vs 存量 1 成员）
	// 组 B：target/c + target/d + other/e（目标 2 成员 vs 存量 1 成员）
	writePNG(t, filepath.Join(libDir, "target", "a.png"), color.RGBA{R: 220, G: 40, B: 40, A: 255}, 64)
	writePNG(t, filepath.Join(libDir, "other", "b.png"), color.RGBA{R: 220, G: 40, B: 40, A: 255}, 64)
	writePNG(t, filepath.Join(libDir, "target", "c.png"), color.RGBA{R: 20, G: 180, B: 20, A: 255}, 32)
	writePNG(t, filepath.Join(libDir, "target", "d.png"), color.RGBA{R: 20, G: 180, B: 20, A: 255}, 32)
	writePNG(t, filepath.Join(libDir, "other", "e.png"), color.RGBA{R: 20, G: 180, B: 20, A: 255}, 32)

	lib := &repo.Library{Name: "对比库", Path: libDir, Kind: "image"}
	if err := lr.Create(lib); err != nil {
		t.Fatal(err)
	}
	// 直接扫描入库（与 TestGenerateDirCompare 使用同一媒体写入路径）
	sessionRepo := repo.NewSessionRepo(dbh)
	scanSvc := &scan.Service{
		Sessions: sessionRepo, Media: mr, Config: cfg,
		ImageThumbBase: filepath.Join(root, "thumbs"), Libraries: lr,
	}
	noop := func(string, int, int, int, int, int, int64, int64, float64, *int64) {}
	if _, err := scanSvc.ExecuteScan(context.Background(), *lib, "session-1", false, false, 2, noop); err != nil {
		t.Fatal(err)
	}

	opts := Options{MediaType: "image", ImageThreshold: 90, IncludeSHA1: true}
	rep, err := svc.GenerateDirCompare(opts, lib.ID, "target", "")
	if err != nil {
		t.Fatal(err)
	}
	if rep.TotalGroups != 2 || rep.TotalFiles != 5 {
		t.Fatalf("目录对比报告应 2 组 5 文件: %+v", rep)
	}

	exist := func(rel string) bool {
		_, err := os.Stat(filepath.Join(libDir, filepath.FromSlash(rel)))
		return err == nil
	}

	// 清除 target 目录：组 A 本目录仅 1 成员（a）→ 直接删除；
	// 组 B 本目录 2 成员（c/d）→ 按保留条件保留 1 个删 1 个；other 成员一律不动。
	res, err := svc.DirClear(ClearRequest{
		Scope: "directory", Keep: "largest", Directory: "target",
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	if res.DeletedFiles != 2 {
		t.Fatalf("清除 target 应删 2 个文件，实际 %d", res.DeletedFiles)
	}
	if exist("target/a.png") {
		t.Fatalf("target/a.png 应已删除（组 A 本目录唯一成员）")
	}
	if !(exist("target/c.png") || exist("target/d.png")) {
		t.Fatalf("组 B 应保留 1 个（c 或 d）")
	}
	if exist("other/b.png") == false || exist("other/e.png") == false {
		t.Fatalf("清除 target 不应影响 other 目录文件")
	}

	// 清除 other 目录：第一次清除 target 后组 A（a/b）已被 prune，b 不再属于任何组，
	// 不参与清除；组 B 本目录仅 1 成员（e）→ 直接删除。
	res, err = svc.DirClear(ClearRequest{
		Scope: "directory", Keep: "largest", Directory: "other",
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	if res.DeletedFiles != 1 {
		t.Fatalf("清除 other 应删 1 个文件（e），实际 %d", res.DeletedFiles)
	}
	if exist("other/e.png") {
		t.Fatalf("other/e.png 应已删除")
	}
	if exist("other/b.png") == false {
		t.Fatalf("b 所在组已被 prune，不应再被删除")
	}
	if !(exist("target/c.png") || exist("target/d.png")) {
		t.Fatalf("清除 other 不应影响 target 目录保留文件")
	}
}
