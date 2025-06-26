package websocket

import (
	"github.com/gorilla/websocket"
	"log"
	"net/http"
	"sync"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // 允许所有来源，生产环境应该配置具体的域名
	},
}

// Server WebSocket服务器
type Server struct {
	// 保护 clients 的互斥锁
	sync.RWMutex
	// 活动的客户端连接
	clients map[string]*Client
}

// NewServer 创建新的WebSocket服务器
func NewServer() *Server {
	return &Server{
		clients: make(map[string]*Client),
	}
}

// HandleConnection 处理新的WebSocket连接
func (s *Server) HandleConnection(w http.ResponseWriter, r *http.Request, userID string) {
	// 升级HTTP连接为WebSocket连接
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("升级WebSocket连接失败: %v", err)
		return
	}

	// 检查用户是否已有活动连接
	s.Lock()
	if existingClient, ok := s.clients[userID]; ok {
		// 关闭现有连接
		existingClient.Close()
		delete(s.clients, userID)
	}

	// 创建新的客户端
	client := NewClient(s, conn, userID)
	s.clients[userID] = client
	s.Unlock()

	// 启动客户端处理
	go client.Run()
}

// RemoveClient 从服务器中移除客户端
func (s *Server) RemoveClient(userID string) {
	s.Lock()
	delete(s.clients, userID)
	s.Unlock()
}

// GetClient 获取指定用户ID的客户端
func (s *Server) GetClient(userID string) *Client {
	s.RLock()
	defer s.RUnlock()
	return s.clients[userID]
}

// BroadcastMessage 向所有客户端广播消息
func (s *Server) BroadcastMessage(message []byte) {
	s.RLock()
	defer s.RUnlock()

	for _, client := range s.clients {
		client.Send(message)
	}
} 