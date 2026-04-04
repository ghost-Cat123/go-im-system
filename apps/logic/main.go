package main

import (
	"GeeRPC"
	"GeeRPC/midware"
	"go-im-system/apps/logic/service"
	"go-im-system/apps/logic/task"
	"go-im-system/apps/pkg/cache"
	"go-im-system/apps/pkg/db"
	"log"
	"net"
)

func main() {
	db.InitMySQL()

	cache.InitRedis()

	task.StartCronJobs()

	GeeRPC.Use(midware.LoggerInterceptor, midware.RecoveryInterceptor)

	_ = GeeRPC.Register(new(service.LogicService))

	l, err := net.Listen("tcp", ":8001")
	if err != nil {
		log.Fatal("network error:", err)
	}
	log.Println("Logic RPC 服务端启动成功，端口 :8001")

	GeeRPC.Accept(l)
}
