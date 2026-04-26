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
	"go-im-system/apps/pkg/mq"
	"go-im-system/apps/pkg/utils"
	"log"
	"net"
	"strconv"
)

func main() {
	// 实际开发中，这个路径可以用命令行 flag 传进来，比如 -c ./config.yaml
	if err := config.InitConfig(""); err != nil {
		log.Fatalf("配置初始化失败: %v", err)
	}

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

	logicSnow := config.GlobalConfig.Server.LogicSnowflakeNode
	if logicSnow == 0 {
		logicSnow = 2
	}
	if err := utils.InitSnowflake(int64(logicSnow)); err != nil {
		logger.Log.Fatalf("Logic Snowflake 初始化失败: %v", err)
	}

	// 初始化 RabbitMQ：Logic 作为消息生产者，落库后将消息发布到目标网关
	if mqErr := mq.InitRabbitMQ(config.GlobalConfig.RabbitMQ.URL); mqErr != nil {
		logger.Log.Fatalf("初始化 RabbitMQ 失败: %v", mqErr)
	}
	defer mq.Close()

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
