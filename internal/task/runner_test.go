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

// TestScanTaskETAUsesWorkBytesAndExcludesSkipped 验证 ETA 按字节口径计算且跳过文件不计入。
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

	// 运行期间轮询：进度闭包按字节计算速率与 ETA（剩余 50000 字节 / 50000 B/s ≈ 1s）
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
	if eta <= 0 {
		t.Fatalf("应按剩余字节计算 ETA: %d", eta)
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
