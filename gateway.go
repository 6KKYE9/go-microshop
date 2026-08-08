package main

import (
	"io"
	"net/http"
	"sync"
	"time"
)

// Gateway 是 API 网关：收外部请求，按服务名查注册中心找到后端实例，
// 做限流和熔断后转发过去，把响应原样返回。
type Gateway struct {
	reg     *Registry
	limiter *Limiter
	client  *http.Client

	mu       sync.Mutex
	rr       map[string]int // 每个服务的轮询计数
	breakers map[string]*Breaker
}

func newGateway(reg *Registry, rate float64, burst int) *Gateway {
	return &Gateway{
		reg:      reg,
		limiter:  newLimiter(rate, burst),
		client:   &http.Client{Timeout: 3 * time.Second},
		rr:       make(map[string]int),
		breakers: make(map[string]*Breaker),
	}
}

// breaker 给每个服务维护一个熔断器（懒创建）。
func (g *Gateway) breaker(name string) *Breaker {
	g.mu.Lock()
	defer g.mu.Unlock()
	b, ok := g.breakers[name]
	if !ok {
		b = newBreaker(5, 10*time.Second)
		g.breakers[name] = b
	}
	return b
}

// pick 按服务名轮询挑一个健康实例地址。
func (g *Gateway) pick(name string) (string, bool) {
	addrs := g.reg.Discover(name)
	if len(addrs) == 0 {
		return "", false
	}
	g.mu.Lock()
	i := g.rr[name] % len(addrs)
	g.rr[name] = i + 1
	g.mu.Unlock()
	return addrs[i], true
}

// ServeHTTP 处理所有进网关的请求，路径形如 /api/<service>/<path>。
func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 先限流：整网关一个桶，超了直接 429。
	if !g.limiter.Allow() {
		writeErr(w, "请求太频繁，限流中", http.StatusTooManyRequests)
		return
	}

	// 从路径里抠出服务名：/api/products/1 -> 服务 products，转发到 /products/1
	rest := trimPrefix(r.URL.Path, "/api/")
	parts := splitFirst(rest)
	svc := parts[0]

	b := g.breaker(svc)
	if !b.Allow() {
		writeErr(w, "服务熔断中，暂时不可用 ("+b.State()+")", http.StatusServiceUnavailable)
		return
	}

	addr, ok := g.pick(svc)
	if !ok {
		writeErr(w, "找不到服务实例: "+svc, http.StatusBadGateway)
		return
	}

	// 转发路径就是去掉 /api/ 之后剩下的整段，保留服务名那段。
	target := "http://" + addr + "/" + rest
	req, err := http.NewRequest(r.Method, target, r.Body)
	if err != nil {
		b.Failure()
		writeErr(w, "构造转发请求失败", http.StatusInternalServerError)
		return
	}
	for k, vs := range r.Header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}

	resp, err := g.client.Do(req)
	if err != nil {
		b.Failure()
		writeErr(w, "转发失败: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	b.Success()

	// 把后端响应搬回来。
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// trimPrefix 去掉前缀，没有就返回原串。
func trimPrefix(s, prefix string) string {
	if len(s) >= len(prefix) && s[:len(prefix)] == prefix {
		return s[len(prefix):]
	}
	return s
}

// splitFirst 按第一个 '/' 把 "a/b/c" 拆成 ["a", "b/c"]。
func splitFirst(s string) [2]string {
	for i := 0; i < len(s); i++ {
		if s[i] == '/' {
			return [2]string{s[:i], s[i+1:]}
		}
	}
	return [2]string{s, ""}
}
