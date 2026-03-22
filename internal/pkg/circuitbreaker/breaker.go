package circuitbreaker

import (
	"errors"
	"sync"
	"time"
)

// State 熔断器状态
type State int

const (
	StateClosed   State = iota // 关闭（正常）
	StateOpen                  // 打开（熔断中）
	StateHalfOpen              // 半开（探测恢复）
)

// entry 滑动窗口内的一条请求记录
type entry struct {
	success bool
	at      time.Time
}

// CircuitBreaker 基于滑动时间窗口失败率的熔断器。
//
// 工作原理：
//  1. 维护一个时间窗口（windowSize）内的请求结果队列
//  2. 窗口内请求数 >= minRequests 且失败率 >= failureRate 时触发熔断（StateOpen）
//  3. 熔断后等待 resetTimeout，切换为半开状态（StateHalfOpen），放行一个请求探测
//  4. 探测成功 → 关闭熔断（StateClosed）；探测失败 → 重新打开
type CircuitBreaker struct {
	windowSize   time.Duration // 统计窗口大小，e.g. 60s
	minRequests  int           // 窗口内最少请求数，不足时不触发熔断，e.g. 5
	failureRate  float64       // 失败率阈值 0~1，e.g. 0.5 表示 50%
	resetTimeout time.Duration // 熔断后多久进入半开，e.g. 30s

	mu       sync.Mutex
	state    State
	window   []entry
	openedAt time.Time // 最近一次进入 StateOpen 的时间
}

// NewCircuitBreaker 创建熔断器。
//
//	windowSize   — 滑动窗口大小，e.g. 60*time.Second
//	minRequests  — 触发判断的最少请求数，e.g. 5
//	failureRate  — 失败率阈值 0~1，e.g. 0.5
//	resetTimeout — 熔断后尝试恢复的等待时间，e.g. 30*time.Second
func NewCircuitBreaker(windowSize time.Duration, minRequests int, failureRate float64, resetTimeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		windowSize:   windowSize,
		minRequests:  minRequests,
		failureRate:  failureRate,
		resetTimeout: resetTimeout,
		state:        StateClosed,
	}
}

var ErrCircuitOpen = errors.New("circuit breaker is open")

// Call 执行受保护的函数调用。
// 熔断打开时直接返回 ErrCircuitOpen，不执行 fn。
func (cb *CircuitBreaker) Call(fn func() error) error {
	if !cb.canProceed() {
		return ErrCircuitOpen
	}
	err := fn()
	cb.record(err == nil)
	return err
}

// GetState 返回当前熔断器状态。
func (cb *CircuitBreaker) GetState() State {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}

// canProceed 判断当前是否允许请求通过。
func (cb *CircuitBreaker) canProceed() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateClosed:
		return true
	case StateOpen:
		if time.Since(cb.openedAt) >= cb.resetTimeout {
			cb.state = StateHalfOpen
			return true
		}
		return false
	case StateHalfOpen:
		return true
	default:
		return false
	}
}

// record 记录一次请求结果，并根据窗口内失败率决定是否变更状态。
func (cb *CircuitBreaker) record(success bool) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	now := time.Now()

	// 追加本次记录
	cb.window = append(cb.window, entry{success: success, at: now})

	// 淘汰窗口外的旧记录
	cutoff := now.Add(-cb.windowSize)
	i := 0
	for i < len(cb.window) && cb.window[i].at.Before(cutoff) {
		i++
	}
	cb.window = cb.window[i:]

	// 半开状态：一次探测决定成败
	if cb.state == StateHalfOpen {
		if success {
			cb.state = StateClosed
			cb.window = cb.window[:0]
		} else {
			cb.state = StateOpen
			cb.openedAt = now
			cb.window = cb.window[:0]
		}
		return
	}

	// 关闭状态：请求数不足时不判断
	if len(cb.window) < cb.minRequests {
		return
	}

	// 计算窗口内失败率
	failures := 0
	for _, e := range cb.window {
		if !e.success {
			failures++
		}
	}
	rate := float64(failures) / float64(len(cb.window))
	if rate >= cb.failureRate {
		cb.state = StateOpen
		cb.openedAt = now
		cb.window = cb.window[:0]
	}
}
