package mq

import (
	"context"
	"fmt"
	"sync"

	amqp "github.com/rabbitmq/amqp091-go"
	"go-im-system/apps/pkg/logger"
)

const exchangeName = "gateway.exchange"

// PushPayload MQ 消息体，Logic 生产、Gateway 消费。
// 字段与前端 chat_push 协议对齐，不涉及数据库字段变更。
type PushPayload struct {
	MsgID      int64  `json:"msg_id"`
	SeqID      int64  `json:"seq_id"`
	SenderID   int64  `json:"sender_id"`
	ReceiverID int64  `json:"receiver_id"`
	Content    string `json:"content"`
	ChatType   string `json:"chat_type"`
}

var (
	conn    *amqp.Connection
	channel *amqp.Channel
	mu      sync.Mutex
)

// InitRabbitMQ 建立连接并声明 Direct Exchange（持久化，重启不消失）。
// Logic 和 Gateway 启动时各自调用一次。
func InitRabbitMQ(url string) error {
	var err error
	conn, err = amqp.Dial(url)
	if err != nil {
		return fmt.Errorf("RabbitMQ 连接失败: %w", err)
	}

	channel, err = conn.Channel()
	if err != nil {
		return fmt.Errorf("创建 Channel 失败: %w", err)
	}

	// Direct Exchange：RoutingKey = 目标 gatewayAddr，精准路由到对应网关 Queue
	err = channel.ExchangeDeclare(
		exchangeName, // name
		"direct",     // type
		true,         // durable：服务重启后 Exchange 仍存在
		false,        // auto-delete
		false,        // internal
		false,        // no-wait
		nil,          // args
	)
	if err != nil {
		return fmt.Errorf("声明 Exchange 失败: %w", err)
	}

	logger.Log.Infof("✅ RabbitMQ 已连接，Exchange [%s] 声明成功", exchangeName)
	return nil
}

// Publish 将消息发布到 Exchange。
// routingKey 传目标网关地址（如 "127.0.0.1:8080"），消息持久化防重启丢失。
func Publish(ctx context.Context, routingKey string, body []byte) error {
	mu.Lock()
	defer mu.Unlock()

	return channel.PublishWithContext(
		ctx,
		exchangeName, // exchange
		routingKey,   // routing key：目标网关地址
		false,        // mandatory
		false,        // immediate
		amqp.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent, // 持久化消息，Broker 重启不丢
			Body:         body,
		},
	)
}

// ConsumeQueue 声明该网关专属的持久化 Queue，绑定到 Exchange，返回消息通道。
// queueName 示例: "gateway.queue.127.0.0.1:8080"
// routingKey 与 Publish 时一致，即 gatewayAddr
func ConsumeQueue(queueName, routingKey string) (<-chan amqp.Delivery, error) {
	q, err := channel.QueueDeclare(
		queueName,
		true,  // durable
		false, // delete when unused
		false, // exclusive
		false, // no-wait
		nil,   // args
	)
	if err != nil {
		return nil, fmt.Errorf("声明 Queue [%s] 失败: %w", queueName, err)
	}

	if err = channel.QueueBind(q.Name, routingKey, exchangeName, false, nil); err != nil {
		return nil, fmt.Errorf("绑定 Queue 到 Exchange 失败: %w", err)
	}

	// auto-ack=false：手动确认，确保消息被真正处理后再从 Queue 移除
	msgs, err := channel.Consume(q.Name, "", false, false, false, false, nil)
	if err != nil {
		return nil, fmt.Errorf("注册消费者失败: %w", err)
	}

	logger.Log.Infof("✅ Queue [%s] 已声明并绑定，RoutingKey: %s", queueName, routingKey)
	return msgs, nil
}

// Close 优雅关闭（供 main 注册 defer）
func Close() {
	if channel != nil {
		_ = channel.Close()
	}
	if conn != nil {
		_ = conn.Close()
	}
}
