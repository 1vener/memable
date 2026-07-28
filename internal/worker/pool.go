// 包 worker：可配置并发数的 Worker Pool，用于扫描任务调度。
// 代码注释使用中文。
package worker

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
)

// Job 单个任务接口。
type Job interface {
	Execute(ctx context.Context) error
}

// Pool 固定并发数的 Worker Pool。
type Pool struct {
	size    int
	jobs    chan Job
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	failed  atomic.Int64
	done    atomic.Int64
	total   int
	mu      sync.Mutex
	started bool
}

// NewPool 创建 Worker Pool；size <= 0 时默认 4。
func NewPool(size int) *Pool {
	if size <= 0 {
		size = 4
	}
	return &Pool{
		size: size,
		jobs: make(chan Job, 256),
	}
}

// Start 启动 worker goroutine。
func (p *Pool) Start(ctx context.Context) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.started {
		return
	}
	p.ctx, p.cancel = context.WithCancel(ctx)
	p.started = true
	for i := 0; i < p.size; i++ {
		p.wg.Add(1)
		go p.worker(i)
	}
	slog.Info("Worker Pool 已启动", "size", p.size)
}

// worker 单个 worker 的消费循环。
func (p *Pool) worker(id int) {
	defer p.wg.Done()
	for {
		select {
		case <-p.ctx.Done():
			return
		case job, ok := <-p.jobs:
			if !ok {
				return
			}
			if err := job.Execute(p.ctx); err != nil {
				slog.Warn("任务执行失败", "worker", id, "err", err)
				p.failed.Add(1)
			}
			p.done.Add(1)
		}
	}
}

// Submit 提交任务；如果 pool 已关闭或取消则返回错误。
func (p *Pool) Submit(job Job) bool {
	select {
	case <-p.ctx.Done():
		return false
	case p.jobs <- job:
		p.total++
		return true
	}
}

// Stop 等待所有任务完成后停止。
func (p *Pool) Stop() {
	close(p.jobs)
	p.wg.Wait()
	if p.cancel != nil {
		p.cancel()
	}
}

// Cancel 立即取消所有任务。
func (p *Pool) Cancel() {
	if p.cancel != nil {
		p.cancel()
	}
}

// Stats 返回当前进度统计。
func (p *Pool) Stats() PoolStats {
	return PoolStats{
		Total:  p.total,
		Done:   int(p.done.Load()),
		Failed: int(p.failed.Load()),
	}
}

// PoolStats Worker Pool 进度统计。
type PoolStats struct {
	Total  int // 提交总数
	Done   int // 完成数（含失败）
	Failed int // 失败数
}

// ScanJob 封装单个文件的扫描任务。
type ScanJob struct {
	Run func(ctx context.Context) error
}

// Execute 实现 Job 接口。
func (j *ScanJob) Execute(ctx context.Context) error {
	return j.Run(ctx)
}
