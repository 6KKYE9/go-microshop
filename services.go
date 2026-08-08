package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
)

// 两个示例后端服务：商品服务和订单服务。都是内存存储，演示网关怎么转发到它们。

// Product 商品。
type Product struct {
	ID    int64   `json:"id"`
	Name  string  `json:"name"`
	Price float64 `json:"price"`
	Stock int     `json:"stock"`
}

// productSvc 商品服务，内存实现。
type productSvc struct {
	mu    sync.Mutex
	next  int64
	items map[int64]Product
}

func newProductSvc() *productSvc {
	return &productSvc{items: make(map[int64]Product)}
}

func (s *productSvc) handle(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/products":
		s.create(w, r)
	case r.Method == http.MethodGet && len(r.URL.Path) > len("/products/"):
		s.get(w, r)
	default:
		writeErr(w, "不支持的路径或方法", http.StatusNotFound)
	}
}

func (s *productSvc) create(w http.ResponseWriter, r *http.Request) {
	var p Product
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil || p.Name == "" {
		writeErr(w, "body 要像 {\"name\":\"\",\"price\":0,\"stock\":0}", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	s.next++
	p.ID = s.next
	s.items[p.ID] = p
	s.mu.Unlock()
	writeJSON(w, p)
}

func (s *productSvc) get(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.URL.Path[len("/products/"):], 10, 64)
	if err != nil {
		writeErr(w, "id 不是数字", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	p, ok := s.items[id]
	s.mu.Unlock()
	if !ok {
		writeErr(w, "商品不存在", http.StatusNotFound)
		return
	}
	writeJSON(w, p)
}

// Order 订单。
type Order struct {
	ID        int64   `json:"id"`
	ProductID int64   `json:"product_id"`
	Qty       int     `json:"qty"`
	Total     float64 `json:"total"`
}

// orderSvc 订单服务，依赖商品服务来算金额和扣库存。
type orderSvc struct {
	mu      sync.Mutex
	next    int64
	orders  map[int64]Order
	product *productSvc
}

func newOrderSvc(p *productSvc) *orderSvc {
	return &orderSvc{orders: make(map[int64]Order), product: p}
}

func (s *orderSvc) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost && r.URL.Path == "/orders" {
		s.create(w, r)
		return
	}
	writeErr(w, "不支持的路径或方法", http.StatusNotFound)
}

func (s *orderSvc) create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProductID int64 `json:"product_id"`
		Qty       int   `json:"qty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Qty <= 0 {
		writeErr(w, "body 要像 {\"product_id\":1,\"qty\":2}", http.StatusBadRequest)
		return
	}

	s.product.mu.Lock()
	p, ok := s.product.items[req.ProductID]
	if !ok {
		s.product.mu.Unlock()
		writeErr(w, "商品不存在", http.StatusNotFound)
		return
	}
	if p.Stock < req.Qty {
		s.product.mu.Unlock()
		writeErr(w, "库存不足", http.StatusConflict)
		return
	}
	p.Stock -= req.Qty
	s.product.items[req.ProductID] = p
	s.product.mu.Unlock()

	s.mu.Lock()
	s.next++
	o := Order{ID: s.next, ProductID: req.ProductID, Qty: req.Qty, Total: p.Price * float64(req.Qty)}
	s.orders[o.ID] = o
	s.mu.Unlock()
	writeJSON(w, o)
}
