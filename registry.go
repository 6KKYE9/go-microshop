package main

import (
	"net/http"
	"sync"
	"time"
)

// Instance 是一个服务实例的注册信息。
type Instance struct {
	Name     string    `json:"name"`
	Addr     string    `json:"addr"`
	LastSeen time.Time `json:"last_seen"`
}

// Registry 是服务注册中心，内存版。服务启动时来注册，定期心跳续命，
// 超时的实例会被清掉。网关转发前先来查有哪些健康实例。
type Registry struct {
	mu        sync.Mutex
	instances map[string][]Instance // 服务名 -> 实例列表
	ttl       time.Duration
}

func newRegistry(ttl time.Duration) *Registry {
	if ttl <= 0 {
		ttl = 15 * time.Second
	}
	r := &Registry{instances: make(map[string][]Instance), ttl: ttl}
	go r.reap()
	return r
}

// Register 注册或刷新一个实例。同一 addr 再次出现就更新 LastSeen。
func (r *Registry) Register(name, addr string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	list := r.instances[name]
	for i := range list {
		if list[i].Addr == addr {
			list[i].LastSeen = time.Now()
			r.instances[name] = list
			return
		}
	}
	list = append(list, Instance{Name: name, Addr: addr, LastSeen: time.Now()})
	r.instances[name] = list
}

// Discover 返回某个服务名下所有还没过期的实例地址。
func (r *Registry) Discover(name string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	now := time.Now()
	for _, ins := range r.instances[name] {
		if now.Sub(ins.LastSeen) <= r.ttl {
			out = append(out, ins.Addr)
		}
	}
	return out
}

// reap 后台清掉超时的实例，避免网关把请求发给死掉的节点。
func (r *Registry) reap() {
	t := time.NewTicker(r.ttl)
	defer t.Stop()
	for range t.C {
		r.mu.Lock()
		now := time.Now()
		for name, list := range r.instances {
			kept := list[:0]
			for _, ins := range list {
				if now.Sub(ins.LastSeen) <= r.ttl {
					kept = append(kept, ins)
				}
			}
			if len(kept) == 0 {
				delete(r.instances, name)
			} else {
				r.instances[name] = kept
			}
		}
		r.mu.Unlock()
	}
}

// RegisterRoutes 挂上注册中心的 HTTP 接口。
func (r *Registry) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/registry/register", func(w http.ResponseWriter, req *http.Request) {
		name := req.URL.Query().Get("name")
		addr := req.URL.Query().Get("addr")
		if name == "" || addr == "" {
			http.Error(w, "需要 name 和 addr 参数", http.StatusBadRequest)
			return
		}
		r.Register(name, addr)
		w.Write([]byte("ok"))
	})
	mux.HandleFunc("/registry/discover", func(w http.ResponseWriter, req *http.Request) {
		name := req.URL.Query().Get("name")
		writeJSON(w, r.Discover(name))
	})
}
