// runner_test.go：后台任务调度器信号与执行回归测试。
package task

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"memable/internal/config"
	"memable/internal/db"
	"memable/internal/media"
	"memable/internal/repo"
)

func TestEnqueueWakesRunnerWithoutStoppingIt(t *testing.T) {
	dbh, err := db.Open(&config.Config{Database: config.DatabaseConfig{Path: ":memory:"}})
	if err != nil {
		t.Fatal(err)
	}
	defer dbh.Close()
	if err := db.Migrate(dbh); err != nil {
		t.Fatal(err)
	}

	tasks := repo.NewTaskRepo(dbh)
	runner := NewRunner(tasks, repo.NewSessionRepo(dbh), repo.NewMediaRepo(dbh),
		repo.NewLibraryRepo(dbh), nil, RunnerConfig{}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner.Start(ctx)
	defer runner.Stop()

	first, err := runner.Enqueue(repo.TaskKindReportImage, "图片重复统计", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	waitTaskTerminal(t, tasks, first.ID)

	// 第一次唤醒不能终止消费循环，后续任务也必须继续被取出执行。
	second, err := runner.Enqueue(repo.TaskKindReportVideo, "视频重复统计", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	waitTaskTerminal(t, tasks, second.ID)
}

// TestReportQueueRunsWhileMainQueueBlocked 验证报告任务在独立队列中执行：
// 主队列扫描任务运行期间，报告任务可立即开始并完成，互不阻塞。
func TestReportQueueRunsWhileMainQueueBlocked(t *testing.T) {
	dbh, err := db.Open(&config.Config{Database: config.DatabaseConfig{Path: ":memory:"}})
	if err != nil {
		t.Fatal(err)
	}
	defer dbh.Close()
	if err := db.Migrate(dbh); err != nil {
		t.Fatal(err)
	}

	tasks := repo.NewTaskRepo(dbh)
	lr := repo.NewLibraryRepo(dbh)
	lib := &repo.Library{Name: "并发库", Path: t.TempDir(), Kind: "image"}
	if err := lr.Create(lib); err != nil {
		t.Fatal(err)
	}

	// 扫描执行器阻塞在 channel 上，模拟长时间运行的扫描任务
	entered := make(chan struct{})
	release := make(chan struct{})
	executor := stubScanExecutor{call: func(ctx context.Context, lib repo.Library, sessionID string, temporary, force bool, poolSize int, progress repo.ProgressFunc) (*repo.ScanResult, error) {
		close(entered)
		<-release
		return &repo.ScanResult{Session: &repo.ScanSession{ID: "s-blocked"}, Found: 0}, nil
	}}

	runner := NewRunner(tasks, repo.NewSessionRepo(dbh), repo.NewMediaRepo(dbh), lr, executor, RunnerConfig{PoolSize: 2}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner.Start(ctx)
	defer runner.Stop()

	// 主队列扫描任务入队并进入运行态（阻塞中）
	scan, err := runner.Enqueue(repo.TaskKindScan, "阻塞扫描", nil, &lib.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("扫描任务未进入执行态")
	}

	// 报告任务应绕过扫描，直接在报告队列中执行并完成
	rep, err := runner.Enqueue(repo.TaskKindReportImage, "图片重复统计", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	waitTaskTerminal(t, tasks, rep.ID)
	done, err := tasks.GetByID(rep.ID)
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != repo.TaskStatusCompleted {
		t.Fatalf("报告任务应在扫描运行期间完成: %+v", done)
	}
	// 主队列扫描仍处于运行中，未被报告任务打断
	if s, _ := tasks.GetByID(scan.ID); s.Status != repo.TaskStatusRunning {
		t.Fatalf("扫描任务不应在报告期间结束: %+v", s)
	}

	// 释放扫描，主队列继续消费
	close(release)
	waitTaskTerminal(t, tasks, scan.ID)
}

func waitTaskTerminal(t *testing.T, tasks *repo.TaskRepo, id string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		task, err := tasks.GetByID(id)
		if err != nil {
			t.Fatal(err)
		}
		if task.Status == repo.TaskStatusCompleted || task.Status == repo.TaskStatusFailed {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("任务 %s 未在超时前进入终态", id)
}

func TestScanSha1TaskFillsMissingSHA1(t *testing.T) {
	dbh, err := db.Open(&config.Config{Database: config.DatabaseConfig{Path: ":memory:"}})
	if err != nil {
		t.Fatal(err)
	}
	defer dbh.Close()
	if err := db.Migrate(dbh); err != nil {
		t.Fatal(err)
	}

	tasks := repo.NewTaskRepo(dbh)
	mr := repo.NewMediaRepo(dbh)
	lr := repo.NewLibraryRepo(dbh)
	sr := repo.NewSessionRepo(dbh)

	dir := t.TempDir()
	filePath := filepath.Join(dir, "videos", "a.mp4")
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatal(err)
	}
	content := []byte("fake video content for sha1 test")
	if err := os.WriteFile(filePath, content, 0o644); err != nil {
		t.Fatal(err)
	}

	lib := &repo.Library{Name: "视频库", Path: dir, Kind: "video"}
	if err := lr.Create(lib); err != nil {
		t.Fatal(err)
	}
	mt := time.Now()
	if err := mr.Upsert(&repo.Media{
		LibraryID:    lib.ID,
		Kind:         "video",
		RelativePath: "videos/a.mp4",
		FileSize:     int64(len(content)),
		Mtime:        mt,
		Sha1:         nil,
	}); err != nil {
		t.Fatal(err)
	}

	runner := NewRunner(tasks, sr, mr, lr, nil, RunnerConfig{PoolSize: 2}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner.Start(ctx)
	defer runner.Stop()

	first, err := runner.Enqueue(repo.TaskKindScanSha1, "补齐 SHA1", nil, &lib.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	waitTaskTerminal(t, tasks, first.ID)

	stored, err := mr.GetByPath(lib.ID, "videos/a.mp4")
	if err != nil || stored == nil {
		t.Fatalf("GetByPath: %+v %v", stored, err)
	}
	if stored.Sha1 == nil || len(*stored.Sha1) != 40 {
		t.Fatalf("sha1 未补齐: %+v", stored)
	}
	expected, err := media.SHA1File(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if *stored.Sha1 != expected {
		t.Fatalf("sha1 不匹配: got %q want %q", *stored.Sha1, expected)
	}

	// 二次执行：没有缺失记录，checked/updated 均为 0
	second, err := runner.Enqueue(repo.TaskKindScanSha1, "补齐 SHA1 二次", nil, &lib.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	waitTaskTerminal(t, tasks, second.ID)
	done, err := tasks.GetByID(second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != repo.TaskStatusCompleted || done.ResultJSON == nil {
		t.Fatalf("二次任务状态异常: %+v", done)
	}
	if !strings.Contains(*done.ResultJSON, `"updated":0`) {
		t.Fatalf("二次任务应无更新: %s", *done.ResultJSON)
	}
}

// stubScanExecutor 供 ETA 测试使用的扫描执行器替身。
type stubScanExecutor struct {
	call func(ctx context.Context, lib repo.Library, sessionID string, temporary, force bool, poolSize int, progress repo.ProgressFunc) (*repo.ScanResult, error)
}

func (s stubScanExecutor) ExecuteScan(ctx context.Context, lib repo.Library, sessionID string, temporary, force bool, poolSize int, progress repo.ProgressFunc) (*repo.ScanResult, error) {
	return s.call(ctx, lib, sessionID, temporary, force, poolSize, progress)
}

// TestScanTaskETAUsesWorkBytesAndExcludesSkipped 验证 ETA 双口径估算：
// 字节口径给出 1s（剩余 50000 字节 / 50000 B/s），文件口径给出约 4.8s
// （剩余 80 文件按跳过比例折算 24 个工作文件 / 5 文件每秒），ETA 取较大值。
func TestScanTaskETAUsesWorkBytesAndExcludesSkipped(t *testing.T) {
	dbh, err := db.Open(&config.Config{Database: config.DatabaseConfig{Path: ":memory:"}})
	if err != nil {
		t.Fatal(err)
	}
	defer dbh.Close()
	if err := db.Migrate(dbh); err != nil {
		t.Fatal(err)
	}

	tasks := repo.NewTaskRepo(dbh)
	lr := repo.NewLibraryRepo(dbh)
	lib := &repo.Library{Name: "ETA库", Path: t.TempDir(), Kind: "image"}
	if err := lr.Create(lib); err != nil {
		t.Fatal(err)
	}
	executor := stubScanExecutor{call: func(ctx context.Context, lib repo.Library, sessionID string, temporary, force bool, poolSize int, progress repo.ProgressFunc) (*repo.ScanResult, error) {
		// 总工作量 100000 字节；第一次上报 0，间隔 1 秒后上报 50000 字节
		// （20 个已决定文件中 14 个跳过，跳过不计入字节与速率）。
		progress("processing", 100, 10, 1, 9, 0, 100000, 0, 0, (*int64)(nil))
		time.Sleep(time.Second)
		progress("processing", 100, 20, 6, 14, 0, 100000, 50000, 0, (*int64)(nil))
		// 保持运行状态一小段时间，让测试有机会读取到本次进度写库结果
		time.Sleep(500 * time.Millisecond)
		return &repo.ScanResult{
			Session:        &repo.ScanSession{ID: "s-eta"},
			Found:          100,
			Imported:       6,
			Skipped:        14,
			TotalBytes:     100000,
			ProcessedBytes: 50000,
		}, nil
	}}
	runner := NewRunner(tasks, repo.NewSessionRepo(dbh), repo.NewMediaRepo(dbh), lr, executor, RunnerConfig{PoolSize: 2}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner.Start(ctx)
	defer runner.Stop()

	task, err := runner.Enqueue(repo.TaskKindScan, "同步扫描", nil, &lib.ID, nil)
	if err != nil {
		t.Fatal(err)
	}

	// 运行期间轮询：字节速率 50000 B/s、文件速率 5 文件/s。
	// 字节口径 ETA≈1s，文件口径（80 剩余文件 × 30% 折算 = 24 个工作文件 / 5 ≈ 4.8s），
	// ETA 取较大值，应 >= 4s。
	var rate float64
	var eta int64
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		running, err := tasks.GetByID(task.ID)
		if err != nil {
			t.Fatal(err)
		}
		if running.ProcessingRate > 0 && running.EtaSeconds != nil && *running.EtaSeconds > 0 {
			rate = running.ProcessingRate
			eta = *running.EtaSeconds
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if rate <= 0 {
		t.Fatalf("应按字节计算速率: %f", rate)
	}
	if eta < 4 {
		t.Fatalf("ETA 应取文件口径的较大值（约 4s），实际 %d", eta)
	}

	waitTaskTerminal(t, tasks, task.ID)

	stored, err := tasks.GetByID(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != repo.TaskStatusCompleted {
		t.Fatalf("任务状态异常: %+v", stored)
	}
	if stored.ResultJSON == nil || !strings.Contains(*stored.ResultJSON, `"processed_bytes":50000`) {
		t.Fatalf("结果应包含已处理字节: %s", *stored.ResultJSON)
	}
}

// TestScanTaskETAFileCountFallback 验证无字节数据时 ETA 退化为文件数口径：
// 剩余文件数按已见跳过比例折算后除以文件速率。
func TestScanTaskETAFileCountFallback(t *testing.T) {
	dbh, err := db.Open(&config.Config{Database: config.DatabaseConfig{Path: ":memory:"}})
	if err != nil {
		t.Fatal(err)
	}
	defer dbh.Close()
	if err := db.Migrate(dbh); err != nil {
		t.Fatal(err)
	}

	tasks := repo.NewTaskRepo(dbh)
	lr := repo.NewLibraryRepo(dbh)
	lib := &repo.Library{Name: "ETA库", Path: t.TempDir(), Kind: "image"}
	if err := lr.Create(lib); err != nil {
		t.Fatal(err)
	}
	executor := stubScanExecutor{call: func(ctx context.Context, lib repo.Library, sessionID string, temporary, force bool, poolSize int, progress repo.ProgressFunc) (*repo.ScanResult, error) {
		// 全程无字节数据（totalBytes/processedBytes 均为 0），只上报文件数。
		// 20 个已决定文件中 14 个跳过 → 文件速率 5 个/s（跳过不计入）。
		progress("processing", 100, 10, 1, 9, 0, 0, 0, 0, (*int64)(nil))
		time.Sleep(time.Second)
		progress("processing", 100, 20, 6, 14, 0, 0, 0, 0, (*int64)(nil))
		time.Sleep(500 * time.Millisecond)
		return &repo.ScanResult{
			Session:  &repo.ScanSession{ID: "s-eta-files"},
			Found:    100,
			Imported: 6,
			Skipped:  14,
		}, nil
	}}
	runner := NewRunner(tasks, repo.NewSessionRepo(dbh), repo.NewMediaRepo(dbh), lr, executor, RunnerConfig{PoolSize: 2}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner.Start(ctx)
	defer runner.Stop()

	task, err := runner.Enqueue(repo.TaskKindScan, "同步扫描", nil, &lib.ID, nil)
	if err != nil {
		t.Fatal(err)
	}

	// 文件速率 5/s；剩余 80 文件按跳过比例 14/20=70% 折算为 24 个工作文件 → ETA≈4.8s
	var eta int64
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		running, err := tasks.GetByID(task.ID)
		if err != nil {
			t.Fatal(err)
		}
		if running.EtaSeconds != nil && *running.EtaSeconds > 0 {
			eta = *running.EtaSeconds
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if eta < 4 {
		t.Fatalf("无字节数据时 ETA 应按文件口径计算（约 4s），实际 %d", eta)
	}

	waitTaskTerminal(t, tasks, task.ID)
}
