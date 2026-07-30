// runner_test.go：后台任务调度器信号与执行回归测试。
package task

import (
	"context"
	"testing"
	"time"

	"memable/internal/config"
	"memable/internal/db"
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
		repo.NewLibraryRepo(dbh), nil, RunnerConfig{})
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
