package cache

import (
	"context"
	"fmt"
	"github.com/go-redis/redis/v8"
)

var RedisClient *redis.Client

func InitRedis() {
	ctx := context.Background()
	RedisClient = redis.NewClient(&redis.Options{
		Addr:     "localhost:6379", // Redis 服务器地址和端口
		Password: "",               // Redis 访问密码，如果没有可以为空字符串
		DB:       1,                // 使用的 Redis 数据库编号，默认为 0
	})

	// 使用 Ping() 方法测试是否成功连接到 Redis 服务器
	pong, err := RedisClient.Ping(ctx).Result()
	if err != nil {
		fmt.Println("Failed to connect to Redis:", err)
		return
	}
	fmt.Println("Connected to Redis:", pong)
}
