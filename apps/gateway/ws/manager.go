package ws

import (
	"github.com/gorilla/websocket"
	"sync"
)

// CliMap 维护全局的websocket连接
type CliMap struct {
	sync.RWMutex
	Clients map[string]*websocket.Conn
}

// NewCliMap 构造函数
func NewCliMap() *CliMap {
	return &CliMap{Clients: make(map[string]*websocket.Conn)}
}

func (m *CliMap) Get(key string) (*websocket.Conn, bool) {
	m.RLock()
	defer m.RUnlock()
	client, exit := m.Clients[key]
	return client, exit
}

func (m *CliMap) Set(key string, client *websocket.Conn) {
	m.Lock()
	defer m.Unlock()
	m.Clients[key] = client
}

func (m *CliMap) Delete(key string) {
	m.Lock()
	defer m.Unlock()
	delete(m.Clients, key)
}

func (m *CliMap) Len() int {
	m.RLock()
	defer m.RUnlock()
	return len(m.Clients)
}
