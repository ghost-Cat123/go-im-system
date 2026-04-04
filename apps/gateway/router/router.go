package router

import (
	"github.com/gin-gonic/gin"
	"go-im-system/apps/gateway/api/user_api"
	"go-im-system/apps/gateway/ws"
)

func SetupRouter() *gin.Engine {
	r := gin.Default()

	_ = r.SetTrustedProxies(nil)

	// 1. RESTful HTTP 路由组 处理短连接
	apiGroup := r.Group("/api")
	{
		apiGroup.POST("/login", user_api.UserLoginHandler)
		apiGroup.POST("/register", user_api.UserRegisterHandler)
	}

	// 2. 处理 websocket长连接路由
	r.GET("/ws", ws.WSHandler)

	return r
}
