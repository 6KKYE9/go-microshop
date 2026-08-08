package main

import (
	"sync"
	"time"
)

// Limiter 是令牌桶限流。每秒往桶里加 rate 个令牌，桶最多存 burst 个。
// 每个请求来取一个令牌，取不到就拒绝（网关层返回 429）。
type Limiter struct {
	mu       sync.Mutex
	rate     float64
	burst    int
	tokens   float64
	lastTick time.Time
}

func newLimiter(rate float64, burst int) *Limiter {
	return &Limiter{
		rate:     rate,
		burst:    burst,
		tokens:   float64(burst),
		lastTick: time.Now(),
	}
}

// Allow 取一个令牌，取到返回 true。
func (l *Limiter) Allow() bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	// 按时间补令牌：elapsed 秒 * rate。
	elapsed := now.Sub(l.lastTick).Seconds()
	l.tokens += elapsed * l.rate
	if l.tokens > float64(l.burst) {
		l.tokens = float64(l.burst)
	}
	l.lastTick = now

	if l.tokens >= 1 {
		l.tokens--
		return true
	}
	return false
}
