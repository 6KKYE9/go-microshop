package main

import (
	"flag"
	"log"
	"net/http"
	"time"
)

func main() {
	regAddr := flag.String("reg", ":9100", "注册中心地址")
	gwAddr := flag.String("gw", ":9101", "网关地址")
	productAddr := flag.String("product", "127.0.0.1:9102", "商品服务地址")
	orderAddr := flag.String("order", "127.0.0.1:9103", "订单服务地址")
	rate := flag.Float64("rate", 20, "网关每秒令牌数（限流）")
	burst := flag.Int("burst", 30, "网关令牌桶容量")
	flag.Parse()

	// 1) 注册中心
	reg := newRegistry(15 * time.Second)
	regMux := http.NewServeMux()
	reg.RegisterRoutes(regMux)
	go func() {
		log.Printf("注册中心 %s", *regAddr)
		log.Fatal(http.ListenAndServe(*regAddr, regMux))
	}()

	// 2) 后端服务
	products := newProductSvc()
	orders := newOrderSvc(products)

	productMux := http.NewServeMux()
	productMux.HandleFunc("/", products.handle)
	go func() {
		log.Printf("商品服务 %s", *productAddr)
		log.Fatal(http.ListenAndServe(*productAddr, productMux))
	}()

	orderMux := http.NewServeMux()
	orderMux.HandleFunc("/", orders.handle)
	go func() {
		log.Printf("订单服务 %s", *orderAddr)
		log.Fatal(http.ListenAndServe(*orderAddr, orderMux))
	}()

	// 3) 把后端服务注册上去（这里直接调内存接口，省一次 HTTP）
	reg.Register("products", *productAddr)
	reg.Register("orders", *orderAddr)

	// 4) 网关
	gw := newGateway(reg, *rate, *burst)
	log.Printf("网关 %s", *gwAddr)
	log.Fatal(http.ListenAndServe(*gwAddr, gw))
}
