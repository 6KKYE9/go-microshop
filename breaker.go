package main

import (
	"sync"
	"time"
)

// 熔断器三个状态。
const (
	stateClosed uint8 = iota
	stateOpen
	stateHalfOpen
)

// Breaker 是简单的熔断实现。连续失败超过阈值就打开（直接拒绝请求），
// 过一段冷却时间后放一个探活请求，成功就恢复，失败就继续打开。
// 避免后端已经挂了还一直把流量打过去，雪崩。
type Breaker struct {
	mu          sync.Mutex
	state       uint8
	failures    int
	successes   int
	threshold   int           // 连续失败多少次打开
	cooldown    time.Duration // 打开后多久转 half-open
	openedAt    time.Time
	halfOpenMax int // half-open 阶段最多放几个探活
}

func newBreaker(threshold int, cooldown time.Duration) *Breaker {
	if threshold <= 0 {
		threshold = 5
	}
	if cooldown <= 0 {
		cooldown = 10 * time.Second
	}
	return &Breaker{state: stateClosed, threshold: threshold, cooldown: cooldown, halfOpenMax: 1}
}

// Allow 返回当前是否放行请求。
func (b *Breaker) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.state {
	case stateClosed:
		return true
	case stateOpen:
		if time.Since(b.openedAt) >= b.cooldown {
			b.state = stateHalfOpen
			b.successes = 0
			b.halfOpenMax = 1
			return true
		}
		return false
	case stateHalfOpen:
		// 只允许有限个探活请求通过。
		if b.successes+b.failures < b.halfOpenMax {
			return true
		}
		return false
	}
	return false
}

// Success 上报一次成功。
func (b *Breaker) Success() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures = 0
	if b.state == stateHalfOpen {
		b.successes++
		if b.successes >= b.halfOpenMax {
			b.state = stateClosed
		}
	}
}

// Failure 上报一次失败。
func (b *Breaker) Failure() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures++
	if b.state == stateHalfOpen {
		b.state = stateOpen
		b.openedAt = time.Now()
		return
	}
	if b.failures >= b.threshold {
		b.state = stateOpen
		b.openedAt = time.Now()
	}
}

func (b *Breaker) State() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.state {
	case stateOpen:
		return "open"
	case stateHalfOpen:
		return "half-open"
	default:
		return "closed"
	}
}
