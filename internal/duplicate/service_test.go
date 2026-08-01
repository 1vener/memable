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

	// 清空 sha1 后不再生成 sha1_exact 分组（pHash 差异大也不会进入相似分组）
	if _, err := dbh.Exec(`UPDATE media SET sha1 = NULL`); err != nil {
		t.Fatal(err)
	}
	groups2, err := det.DetectWithOptions(Options{MediaType: "video", IncludeSHA1: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(groups2) != 0 {
		t.Fatalf("无 sha1 时不应生成分组: %+v", groups2)
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

	svc := NewService(dupRepo, mediaRepo, libRepo, cfg, thumbDir, thumbDir)

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
	tree, err := svc.Tree()
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
	remainingTree, err := svc.Tree()
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
