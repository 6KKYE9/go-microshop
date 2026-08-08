package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestGatewayFlow(t *testing.T) {
	reg := newRegistry(15 * time.Second)
	products := newProductSvc()
	orders := newOrderSvc(products)

	// 两个后端
	pm := http.NewServeMux()
	pm.HandleFunc("/", products.handle)
	ps := httptest.NewServer(pm)
	defer ps.Close()
	om := http.NewServeMux()
	om.HandleFunc("/", orders.handle)
	osrv := httptest.NewServer(om)
	defer osrv.Close()

	// 注册中心记上（去掉 http:// 前缀，模拟真实 addr）
	reg.Register("products", strings.TrimPrefix(ps.URL, "http://"))
	reg.Register("orders", strings.TrimPrefix(osrv.URL, "http://"))

	gw := newGateway(reg, 1000, 1000)
	gm := http.NewServeMux()
	gm.Handle("/", gw)
	gs := httptest.NewServer(gm)
	defer gs.Close()

	// 建一个商品
	resp, _ := http.Post(gs.URL+"/api/products", "application/json", strings.NewReader(`{"name":"键盘","price":99.5,"stock":10}`))
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("建商品状态码 %d", resp.StatusCode)
	}
	var p Product
	json.NewDecoder(resp.Body).Decode(&p)
	if p.ID == 0 || p.Stock != 10 {
		t.Fatalf("商品数据异常: %+v", p)
	}

	// 查商品
	get, _ := http.Get(gs.URL + "/api/products/" + itoa(p.ID))
	defer get.Body.Close()
	if get.StatusCode != 200 {
		t.Fatalf("查商品状态码 %d", get.StatusCode)
	}

	// 下订单
	od, _ := http.Post(gs.URL+"/api/orders", "application/json", strings.NewReader(`{"product_id":`+itoa(p.ID)+`,"qty":2}`))
	defer od.Body.Close()
	if od.StatusCode != 200 {
		t.Fatalf("下单状态码 %d", od.StatusCode)
	}
	var o Order
	json.NewDecoder(od.Body).Decode(&o)
	if o.Total != 199.0 {
		t.Fatalf("订单金额应为 199, 得到 %v", o.Total)
	}

	// 库存应扣到 8
	get2, _ := http.Get(gs.URL + "/api/products/" + itoa(p.ID))
	defer get2.Body.Close()
	var p2 Product
	json.NewDecoder(get2.Body).Decode(&p2)
	if p2.Stock != 8 {
		t.Fatalf("库存应扣到 8, 实际 %d", p2.Stock)
	}
}

func TestLimiterKicksIn(t *testing.T) {
	l := newLimiter(10, 2) // 桶容量 2
	if !l.Allow() || !l.Allow() {
		t.Fatal("前两个该放行")
	}
	if l.Allow() {
		t.Fatal("桶空了还放行，限流失效")
	}
	// 等够补令牌再试
	time.Sleep(300 * time.Millisecond)
	if !l.Allow() {
		t.Fatal("补令牌后该能放行")
	}
}

func TestBreakerOpens(t *testing.T) {
	b := newBreaker(3, 50*time.Millisecond)
	for i := 0; i < 3; i++ {
		if !b.Allow() {
			t.Fatal("未达阈值前应放行")
		}
		b.Failure()
	}
	if b.Allow() {
		t.Fatal("达到阈值后应熔断拒绝")
	}
	// 冷却后转 half-open，应放行一个探活
	time.Sleep(60 * time.Millisecond)
	if !b.Allow() {
		t.Fatal("冷却后 half-open 应放行探活")
	}
	b.Success()
	if b.State() != "closed" {
		t.Fatalf("探活成功后应恢复 closed, 实际 %s", b.State())
	}
}

func TestRegistryTTL(t *testing.T) {
	reg := newRegistry(50 * time.Millisecond)
	reg.Register("svc", "1.2.3.4:1111")
	if len(reg.Discover("svc")) != 1 {
		t.Fatal("刚注册应能发现")
	}
	time.Sleep(120 * time.Millisecond)
	if len(reg.Discover("svc")) != 0 {
		t.Fatal("过期后应被清理")
	}
}

func itoa(i int64) string {
	return strconv.FormatInt(i, 10)
}
