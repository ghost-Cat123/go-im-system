package rpcclient

import (
	"GeeRPC"
	"GeeRPC/codec"
	"GeeRPC/xclient"
	"time"
)

// LogicRpcClient 创建全局RPC客户端
// 使用封装好的负载均衡的+注册中心客户端
var LogicRpcClient *xclient.XClient

func InitRPCClient() {
	// 注册中心维护的服务发现
	discovery := xclient.NewMultiServerDiscovery([]string{"tcp@localhost:8001"})
	// 强制指定编解码器为 Protobuf
	opt := &GeeRPC.Option{
		MagicNumber:    GeeRPC.MagicNumber,
		CodecType:      codec.ProtobufType,
		ConnectTimeout: 10 * time.Second,
	}
	LogicRpcClient = xclient.NewXClient(discovery, xclient.ConsistentHash, opt)
}
