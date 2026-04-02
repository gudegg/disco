package main

import (
	"encoding/json"
	"sync"
	"time"

	"config-center/models"
)

// SSEManager SSE 连接管理器
type SSEManager struct {
	clients map[string]map[chan string]bool // key: service:env -> channels
	mu      sync.RWMutex
}

// NewSSEManager 创建 SSE 管理器
func NewSSEManager() *SSEManager {
	return &SSEManager{
		clients: make(map[string]map[chan string]bool),
	}
}

// Subscribe 订阅配置变更
func (m *SSEManager) Subscribe(service, env string) chan string {
	key := service + ":" + env
	ch := make(chan string, 10)

	m.mu.Lock()
	if m.clients[key] == nil {
		m.clients[key] = make(map[chan string]bool)
	}
	m.clients[key][ch] = true
	m.mu.Unlock()

	return ch
}

// Unsubscribe 取消订阅
func (m *SSEManager) Unsubscribe(service, env string, ch chan string) {
	key := service + ":" + env

	m.mu.Lock()
	if clients, ok := m.clients[key]; ok {
		delete(clients, ch)
		if len(clients) == 0 {
			delete(m.clients, key)
		}
	}
	m.mu.Unlock()

	close(ch)
}

// Broadcast 广播配置变更
func (m *SSEManager) Broadcast(service, env string, message string) {
	key := service + ":" + env

	m.mu.RLock()
	clients := m.clients[key]
	m.mu.RUnlock()

	for ch := range clients {
		select {
		case ch <- message:
		default:
			// 通道满了，跳过
		}
	}
}

// BroadcastConfigChange 广播配置变更消息
func (m *SSEManager) BroadcastConfigChange(service, env string, version int) {
	msg := models.ConfigChangeMessage{
		Type:    "config_changed",
		Service: service,
		Env:     env,
		Version: version,
	}
	data, _ := json.Marshal(msg)
	m.Broadcast(service, env, string(data))
}

// StartHeartbeat 启动心跳
func (m *SSEManager) StartHeartbeat() {
	ticker := time.NewTicker(30 * time.Second)
	go func() {
		for range ticker.C {
			msg := map[string]string{"type": "heartbeat"}
			data, _ := json.Marshal(msg)

			m.mu.RLock()
			allClients := make(map[chan string]bool)
			for _, clients := range m.clients {
				for ch := range clients {
					allClients[ch] = true
				}
			}
			m.mu.RUnlock()

			for ch := range allClients {
				select {
				case ch <- string(data):
				default:
				}
			}
		}
	}()
}

var sseManager = NewSSEManager()

// GetSSEManager 获取 SSE 管理器
func GetSSEManager() *SSEManager {
	return sseManager
}
