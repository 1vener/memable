// tracker.go：任务进度聚合器（内存 EWMA 速度 + ETA，定期刷新 SQLite）。
// 代码注释使用中文。
package task

import (
	"sync"
	"time"

	"memable/internal/repo"
)

// ProgressTracker 任务进度聚合器。
// Worker 只更新内存计数，由单独 goroutine 定期刷新到 SQLite，避免频繁写库。
type ProgressTracker struct {
	mu sync.Mutex

	phase     string
	total     int
	processed int
	succeeded int
	skipped   int
	failed    int

	// EWMA 速度
	lastProcessed  int
	lastSampleTime time.Time
	ewmaRate       float64

	done chan struct{}
}

// NewProgressTracker 创建进度聚合器并启动定期刷新 goroutine。
// interval 为刷新间隔；taskRepo 用于写库；taskID 为目标任务 ID。
func NewProgressTracker(taskRepo *repo.TaskRepo, taskID string, interval time.Duration) *ProgressTracker {
	t := &ProgressTracker{done: make(chan struct{})}
	go t.flushLoop(taskRepo, taskID, interval)
	return t
}

// SetPhase 设置阶段名称。
func (t *ProgressTracker) SetPhase(phase string) {
	t.mu.Lock()
	t.phase = phase
	t.mu.Unlock()
}

// SetTotal 设置总文件数。
func (t *ProgressTracker) SetTotal(total int) {
	t.mu.Lock()
	t.total = total
	t.mu.Unlock()
}

// MarkSucceeded 标记成功处理一个文件。
func (t *ProgressTracker) MarkSucceeded() {
	t.mu.Lock()
	t.succeeded++
	t.processed = t.succeeded + t.skipped + t.failed
	t.updateEWMA()
	t.mu.Unlock()
}

// MarkSkipped 标记跳过一个文件。
func (t *ProgressTracker) MarkSkipped() {
	t.mu.Lock()
	t.skipped++
	t.processed = t.succeeded + t.skipped + t.failed
	t.updateEWMA()
	t.mu.Unlock()
}

// MarkFailed 标记失败一个文件。
func (t *ProgressTracker) MarkFailed() {
	t.mu.Lock()
	t.failed++
	t.processed = t.succeeded + t.skipped + t.failed
	t.updateEWMA()
	t.mu.Unlock()
}

// Stop 停止定期刷新，强迫最后一次写入。
func (t *ProgressTracker) Stop() {
	close(t.done)
}

// snapshot 线程安全地获取当前进度快照。
func (t *ProgressTracker) snapshot() repo.TaskProgress {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.buildProgress()
}

// buildProgress 在锁内构造 TaskProgress。
func (t *ProgressTracker) buildProgress() repo.TaskProgress {
	p := repo.TaskProgress{
		Phase:          t.phase,
		Total:          t.total,
		Processed:      t.processed,
		Succeeded:      t.succeeded,
		Skipped:        t.skipped,
		Failed:         t.failed,
		ProcessingRate: t.ewmaRate,
	}
	if t.total > 0 && t.processed > 0 && t.ewmaRate > 0 {
		remaining := t.total - t.processed
		eta := int64(float64(remaining) / t.ewmaRate)
		p.EtaSeconds = &eta
	}
	return p
}

// updateEWMA 在锁内更新速度（仅当取样间隔 >= 500ms）。
func (t *ProgressTracker) updateEWMA() {
	now := time.Now()
	if t.lastSampleTime.IsZero() {
		t.lastProcessed = t.processed
		t.lastSampleTime = now
		return
	}
	elapsed := now.Sub(t.lastSampleTime).Seconds()
	if elapsed < 0.5 {
		return // 节流
	}
	instant := float64(t.processed-t.lastProcessed) / elapsed
	if t.ewmaRate == 0 {
		t.ewmaRate = instant
	} else {
		t.ewmaRate = 0.25*instant + 0.75*t.ewmaRate
	}
	t.lastProcessed = t.processed
	t.lastSampleTime = now

	// 如果 10s 内没有新进度，清零速度和 ETA
	if float64(t.processed-t.lastProcessed) == 0 && elapsed > 10 {
		t.ewmaRate = 0
	}
}

// flushLoop 定期将进度刷新到 SQLite。
func (t *ProgressTracker) flushLoop(taskRepo *repo.TaskRepo, taskID string, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			_ = taskRepo.UpdateProgress(taskID, t.snapshot())
		case <-t.done:
			// 最终强制刷新
			_ = taskRepo.UpdateProgress(taskID, t.snapshot())
			return
		}
	}
}
