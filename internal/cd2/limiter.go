// limiter.go：CloudDrive2 请求全局限速器。
// 所有 CD2 调用（验证/目录树/补齐 SHA1 任务）共享同一限速器，避免并发绕过限速。
// 实际间隔 = 基准 + 随机 jitter（+500ms/-200ms）；interval<=0 表示不限速（测试用）。
// 代码注释使用中文。
package cd2

import (
	"context"
	"math/rand"
	"sync"
	"time"
)

// RateLimiter 进程级请求限速器：串行放行 + 基准间隔 + 随机 jitter。
type RateLimiter struct {
	mu       sync.Mutex
	interval time.Duration
	last     time.Time
	rand     *rand.Rand
}

var (
	globalMu    sync.RWMutex
	globalLimit *RateLimiter
)

// SetRateLimit 设置全局限速（0 或负数=不限速，测试用）。
func SetRateLimit(intervalMs int) {
	globalMu.Lock()
	defer globalMu.Unlock()
	if intervalMs <= 0 {
		globalLimit = nil
		return
	}
	globalLimit = &RateLimiter{
		interval: time.Duration(intervalMs) * time.Millisecond,
		rand:     rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// globalLimiter 返回当前全局限速器。
func globalLimiter() *RateLimiter {
	globalMu.RLock()
	defer globalMu.RUnlock()
	return globalLimit
}

// Wait 阻塞到允许发起下一次请求；ctx 取消可中断。
// 实际间隔在 [基准-200ms, 基准+500ms] 内随机浮动。
func (l *RateLimiter) Wait(ctx context.Context) error {
	if l == nil || l.interval <= 0 {
		return nil
	}
	for {
		l.mu.Lock()
		wait := l.interval + time.Duration(l.rand.Int63n(701)-200)*time.Millisecond
		if wait < 0 {
			wait = 0
		}
		d := time.Since(l.last)
		if d >= wait {
			l.last = time.Now()
			l.mu.Unlock()
			return nil
		}
		sleep := wait - d
		l.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(sleep):
		}
	}
}
