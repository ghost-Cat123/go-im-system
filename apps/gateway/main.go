package main

import (
	"github.com/gin-gonic/gin"
	"go-im-system/apps/gateway/router"
	"go-im-system/apps/gateway/rpcclient"
	"go-im-system/apps/pkg/cache"
	"go-im-system/apps/pkg/db"
)

// 入口函数
func main() {

	// 初始化DB连接
	db.InitMySQL()

	// 初始化redis客户端
	cache.InitRedis()

	// 初始化RPC客户端
	rpcclient.InitRPCClient()

	// 启动Gin服务器端
	r := gin.Default()

	// 绑定路由
	r = router.SetupRouter()

	// 启动 HTTP服务器 监听端口
	err := r.Run(":8080")
	if err != nil {
		return
	}
}
