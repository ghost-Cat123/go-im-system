package main

import (
	"GeeRPC"
	"GeeRPC/midware"
	"go-im-system/apps/logic/service"
	"go-im-system/apps/logic/task"
	"go-im-system/apps/pkg/cache"
	"go-im-system/apps/pkg/config"
	"go-im-system/apps/pkg/db"
	"go-im-system/apps/pkg/logger"
	"log"
	"net"
	"strconv"
)

func main() {
	// 实际开发中，这个路径可以用命令行 flag 传进来，比如 -c ./config.yaml
	config.InitConfig("../config.yaml")

	// 初始化生产级日志
	logger.InitLogger()

	// 确保程序退出时把缓冲区里的日志落盘
	defer logger.Log.Sync()

	dbInitErr := db.InitMySQL(config.GlobalConfig.MySQL)
	if dbInitErr != nil {
		logger.Log.Fatalf("连接数据库失败: %v", dbInitErr)
	}

	cacheInitErr := cache.InitRedis(config.GlobalConfig.Redis)
	if cacheInitErr != nil {
		logger.Log.Fatalf("连接缓存失败: %v", cacheInitErr)
	}

	task.StartCronJobs()

	GeeRPC.Use(midware.LoggerInterceptor, midware.RecoveryInterceptor)

	_ = GeeRPC.Register(new(service.LogicService))

	l, err := net.Listen("tcp", ":"+strconv.Itoa(config.GlobalConfig.Server.LogicPort))
	if err != nil {
		log.Fatal("network error:", err)
	}
	logger.Log.Info("Logic RPC 服务端启动成功，端口 :", strconv.Itoa(config.GlobalConfig.Server.LogicPort))

	GeeRPC.Accept(l)
}
