package main

import (
	"github.com/gin-gonic/gin"
	"go-im-system/apps/gateway/router"
	"go-im-system/apps/gateway/rpcclient"
	"go-im-system/apps/pkg/cache"
	"go-im-system/apps/pkg/config"
	"go-im-system/apps/pkg/db"
	"go-im-system/apps/pkg/logger"
	"strconv"
)

// 入口函数
func main() {
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

	// 初始化RPC客户端
	rpcclient.InitRPCClient()

	// 启动Gin服务器端
	r := gin.Default()

	// 绑定路由
	r = router.SetupRouter()

	// 启动 HTTP服务器 监听端口
	err := r.Run(":" + strconv.Itoa(config.GlobalConfig.Server.GatewayPort))
	if err != nil {
		return
	}
}
