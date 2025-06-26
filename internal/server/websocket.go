package server

import (
	"NetherLink-server/config"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
	"log"
	"net/http"
	"sync"
	"time"
	"NetherLink-server/pkg/database"
	"NetherLink-server/internal/model"
)

// WSConnection WebSocket连接的包装结构
type WSConnection struct {
	conn      *websocket.Conn
	isAuth    bool
	uid       string
	authTimer *time.Timer
}

// WSServer WebSocket服务器结构
type WSServer struct {
	engine      *gin.Engine
	upgrader    websocket.Upgrader
	connections sync.Map // map[string]*WSConnection
}

// WSMessage WebSocket消息结构
type WSMessage struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// LoginPayload 登录消息的payload结构
type LoginPayload struct {
	UID   string `json:"uid"`
	Token string `json:"token"`
}

// ChatPayload 聊天消息的payload结构
type ChatPayload struct {
	To      string `json:"to"`
	Content string `json:"content"`
	Type    string `json:"type"`
	Extra   string `json:"extra"`
	IsGroup bool   `json:"is_group"`
}

// FriendRequestPayload 好友请求的payload结构
type FriendRequestPayload struct {
	ToUID   string `json:"to_uid"`
	Message string `json:"message"`
}

// FriendRequestResponse 好友请求的响应结构
type FriendRequestResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// FriendRequestNotification 好友请求的通知结构
type FriendRequestNotification struct {
	RequestID  int64     `json:"request_id"`
	FromUID    string    `json:"from_uid"`
	FromName   string    `json:"from_name"`
	FromAvatar string    `json:"from_avatar"`
	Message    string    `json:"message"`
	CreatedAt  string    `json:"created_at"`
}

// FriendRequestHandlePayload 处理好友请求的payload结构
type FriendRequestHandlePayload struct {
	RequestID int64  `json:"request_id"`
	Action    string `json:"action"` // "accept" 或 "reject"
}

// FriendRequestResultNotification 好友申请结果通知结构
type FriendRequestResultNotification struct {
	RequestID int64  `json:"request_id"`
	FromUID   string `json:"from_uid"`
	FromName  string `json:"from_name"`
	Action    string `json:"action"`
	Message   string `json:"message"`
}

// ChatResponse 聊天消息的响应结构
type ChatResponse struct {
	Success      bool      `json:"success"`
	Message      string    `json:"message"`
	MessageID    int64     `json:"message_id"`
	From         string    `json:"from"`
	Content      string    `json:"content"`
	Type         string    `json:"type"`
	Extra        string    `json:"extra"`
	Timestamp    time.Time `json:"timestamp"`
	Conversation string    `json:"conversation"`
	IsGroup      bool      `json:"is_group"`
}

// GroupJoinRequestPayload 群聊申请的payload结构
type GroupJoinRequestPayload struct {
	GroupID int    `json:"group_id"`
	Message string `json:"message"`
}

// GroupJoinRequestResponse 群聊申请的响应结构
type GroupJoinRequestResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// GroupJoinRequestNotification 群聊申请的通知结构
type GroupJoinRequestNotification struct {
	RequestID    int64     `json:"request_id"`
	GroupID      int       `json:"group_id"`
	GroupName    string    `json:"group_name"`
	FromUID      string    `json:"from_uid"`
	FromName     string    `json:"from_name"`
	FromAvatar   string    `json:"from_avatar"`
	Message      string    `json:"message"`
	CreatedAt    string    `json:"created_at"`
}

// GroupJoinRequestHandlePayload 处理群聊申请的payload结构
type GroupJoinRequestHandlePayload struct {
	RequestID int64  `json:"request_id"`
	Action    string `json:"action"` // "accept" 或 "reject"
}

// GroupJoinRequestResultNotification 群聊申请结果通知结构
type GroupJoinRequestResultNotification struct {
	RequestID    int64  `json:"request_id"`
	GroupID      int    `json:"group_id"`
	GroupName    string `json:"group_name"`
	HandlerUID   string `json:"handler_uid"`
	HandlerName  string `json:"handler_name"`
	HandlerRole  string `json:"handler_role"`
	Action       string `json:"action"`
	Message      string `json:"message"`
}

// NewWSServer 创建新的WebSocket服务器
func NewWSServer() *WSServer {
	s := &WSServer{
		engine: gin.Default(),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
	}
	s.setupRoutes()
	return s
}

func (s *WSServer) setupRoutes() {
	s.engine.GET("/ws", s.handleWebSocket)
}

func (s *WSServer) Run() error {
	return s.engine.Run(":8081")
}

func (s *WSServer) handleWebSocket(c *gin.Context) {
	conn, err := s.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("WebSocket升级失败: %v", err)
		return
	}

	wsConn := &WSConnection{
		conn:   conn,
		isAuth: false,
	}

	// 设置10秒登录超时
	wsConn.authTimer = time.AfterFunc(10*time.Second, func() {
		if !wsConn.isAuth {
			log.Printf("连接超时未登录，断开连接")
			conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"error","payload":{"message":"登录超时"}}`))
			conn.Close()
		}
	})

	// 处理连接
	go s.handleConnection(wsConn)
}

func (s *WSServer) handleConnection(wsConn *WSConnection) {
	defer func() {
		wsConn.conn.Close()
		if wsConn.authTimer != nil {
			wsConn.authTimer.Stop()
		}
		if wsConn.uid != "" {
			s.connections.Delete(wsConn.uid)
		}
	}()

	for {
		// 读取消息
		_, message, err := wsConn.conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				// 正常关闭，不需要打印错误日志
				return
			}
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("连接异常关闭: %v", err)
			}
			break
		}

		// 解析消息
		var msg WSMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			s.sendError(wsConn, "无效的消息格式")
			continue
		}

		// 处理消息
		if err := s.handleMessage(wsConn, &msg); err != nil {
			s.sendError(wsConn, err.Error())
			if !wsConn.isAuth || err.Error() == "认证失败" {
				break // 未登录或认证失败时断开连接
			}
		}
	}
}

func (s *WSServer) handleMessage(wsConn *WSConnection, msg *WSMessage) error {
	// 未登录状态只允许处理登录消息
	if !wsConn.isAuth && msg.Type != "login" {
		return errors.New("请先登录")
	}

	switch msg.Type {
	case "login":
		return s.handleLogin(wsConn, msg.Payload)
	case "chat":
		return s.handleChat(wsConn, msg.Payload)
	case "friend_request":
		return s.handleFriendRequest(wsConn, msg.Payload)
	case "friend_request_handle":
		return s.handleFriendRequestResponse(wsConn, msg.Payload)
	case "group_join_request":
		return s.handleGroupJoinRequest(wsConn, msg.Payload)
	case "group_join_request_handle":
		return s.handleGroupJoinRequestResponse(wsConn, msg.Payload)
	default:
		return errors.New("未知的消息类型")
	}
}

func (s *WSServer) handleLogin(wsConn *WSConnection, payload json.RawMessage) error {
	// 解析登录信息
	var loginPayload LoginPayload
	if err := json.Unmarshal(payload, &loginPayload); err != nil {
		return errors.New("无效的登录信息")
	}

	// 验证token
	claims := jwt.MapClaims{}
	token, err := jwt.ParseWithClaims(loginPayload.Token, claims, func(token *jwt.Token) (interface{}, error) {
		return []byte(config.GlobalConfig.JWT.Secret), nil
	})

	if err != nil || !token.Valid {
		return errors.New("认证失败")
	}

	// 验证uid
	uid, ok := claims["uid"].(string)
	if !ok || uid != loginPayload.UID {
		return errors.New("认证失败")
	}

	// 处理重复登录
	if oldConn, loaded := s.connections.LoadOrStore(uid, wsConn); loaded {
		if oldWsConn, ok := oldConn.(*WSConnection); ok {
			// 发送被踢下线消息
			oldWsConn.conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"error","payload":{"message":"账号在其他设备登录"}}`))
			oldWsConn.conn.Close()
		}
		s.connections.Store(uid, wsConn)
	}

	// 设置连接状态
	wsConn.isAuth = true
	wsConn.uid = uid
	wsConn.authTimer.Stop()

	// 发送登录成功消息
	wsConn.conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"login_success"}`))
	return nil
}

func (s *WSServer) handleChat(wsConn *WSConnection, payload json.RawMessage) error {
	// 解析聊天消息
	var chatPayload ChatPayload
	if err := json.Unmarshal(payload, &chatPayload); err != nil {
		return errors.New("无效的聊天消息格式")
	}

	// 验证必要字段
	if chatPayload.To == "" || chatPayload.Content == "" {
		return errors.New("缺少必要字段")
	}

	// 仅处理文本消息
	if chatPayload.Type != "text" {
		return errors.New("暂不支持的消息类型")
	}

	// 生成消息ID（使用时间戳和随机数）
	messageID := time.Now().UnixNano()
	timestamp := time.Now()
	conversationID := getConversationID(wsConn.uid, chatPayload.To)

	// 准备响应消息
	response := ChatResponse{
		Success:      true,
		Message:      "发送成功",
		MessageID:    messageID,
		From:         wsConn.uid,
		Content:      chatPayload.Content,
		Type:         chatPayload.Type,
		Extra:        chatPayload.Extra,
		Timestamp:    timestamp,
		Conversation: conversationID,
		IsGroup:      false,
	}

	// 发送响应给发送者
	responseData, err := json.Marshal(response)
	if err != nil {
		return errors.New("生成响应消息失败")
	}
	
	responseMsg := WSMessage{
		Type:    "chat_response",
		Payload: responseData,
	}
	
	responseBytes, err := json.Marshal(responseMsg)
	if err != nil {
		return errors.New("生成响应消息失败")
	}
	
	if err := wsConn.conn.WriteMessage(websocket.TextMessage, responseBytes); err != nil {
		return errors.New("发送响应消息失败")
	}

	// 如果接收者在线，发送消息给接收者
	if receiverConn, ok := s.connections.Load(chatPayload.To); ok {
		if wsReceiver, ok := receiverConn.(*WSConnection); ok {
			// 先将响应转换为json.RawMessage
			receiverData, err := json.Marshal(response)
			if err != nil {
				return errors.New("生成接收者消息失败")
			}
			
			receiverMsg := WSMessage{
				Type:    "chat",
				Payload: receiverData,
			}
			
			receiverBytes, err := json.Marshal(receiverMsg)
			if err != nil {
				return errors.New("生成接收者消息失败")
			}
			
			if err := wsReceiver.conn.WriteMessage(websocket.TextMessage, receiverBytes); err != nil {
				return errors.New("发送消息给接收者失败")
			}
		}
	}

	// TODO: 将消息保存到数据库（这部分代码保持不变，等待数据库结构确定后实现）

	return nil
}

func (s *WSServer) handleFriendRequest(wsConn *WSConnection, payload json.RawMessage) error {
	// 解析请求
	var requestPayload FriendRequestPayload
	if err := json.Unmarshal(payload, &requestPayload); err != nil {
		return errors.New("无效的请求格式")
	}

	// 获取数据库连接
	db, err := database.GetDB()
	if err != nil {
		return errors.New("数据库连接失败")
	}

	// 验证必要字段
	if requestPayload.ToUID == "" {
		return errors.New("缺少必要字段")
	}

	// 不能添加自己为好友
	if requestPayload.ToUID == wsConn.uid {
		return errors.New("不能添加自己为好友")
	}

	// 检查目标用户是否存在
	var toUser model.User
	if err := db.Where("uid = ?", requestPayload.ToUID).First(&toUser).Error; err != nil {
		return errors.New("用户不存在")
	}

	// 检查是否已经是好友
	var existingFriend model.Friend
	if err := db.Where("(user_id = ? AND friend_id = ?) OR (user_id = ? AND friend_id = ?)",
		wsConn.uid, requestPayload.ToUID, requestPayload.ToUID, wsConn.uid).First(&existingFriend).Error; err == nil {
		return errors.New("已经是好友关系")
	}

	// 检查是否有待处理的好友请求
	var existingRequest model.FriendRequest
	err = db.Where("from_uid = ? AND to_uid = ? AND status = ?",
		wsConn.uid, requestPayload.ToUID, "pending").First(&existingRequest).Error
	if err == nil {
		return errors.New("已有待处理的好友请求")
	}

	// 检查是否被拒绝且未重新申请
	var rejectedRequest model.FriendRequest
	err = db.Where("from_uid = ? AND to_uid = ? AND status = ?",
		wsConn.uid, requestPayload.ToUID, "rejected").Order("updated_at desc").First(&rejectedRequest).Error
	if err == nil {
		// 如果有被拒绝的请求，允许重新申请
	}

	// 获取发送者信息用于通知
	var fromUser model.User
	if err := db.Where("uid = ?", wsConn.uid).First(&fromUser).Error; err != nil {
		return errors.New("获取用户信息失败")
	}

	// 创建好友请求
	friendReq := model.FriendRequest{
		FromUID:   wsConn.uid,
		ToUID:     requestPayload.ToUID,
		Message:   requestPayload.Message,
		Status:    "pending",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := db.Create(&friendReq).Error; err != nil {
		return errors.New("创建好友请求失败")
	}

	// 发送响应给请求者
	response := FriendRequestResponse{
		Success: true,
		Message: "好友请求已发送",
	}
	responseData, _ := json.Marshal(response)
	responseMsg := WSMessage{
		Type:    "friend_request_response",
		Payload: responseData,
	}
	responseBytes, _ := json.Marshal(responseMsg)
	wsConn.conn.WriteMessage(websocket.TextMessage, responseBytes)

	// 如果接收者在线，发送通知
	if receiverConn, ok := s.connections.Load(requestPayload.ToUID); ok {
		if wsReceiver, ok := receiverConn.(*WSConnection); ok {
			notification := FriendRequestNotification{
				RequestID:  friendReq.RequestID,
				FromUID:    wsConn.uid,
				FromName:   fromUser.Name,
				FromAvatar: fromUser.AvatarURL,
				Message:    requestPayload.Message,
				CreatedAt:  friendReq.CreatedAt.Format("2006-01-02 15:04:05"),
			}
			notificationData, _ := json.Marshal(notification)
			notificationMsg := WSMessage{
				Type:    "friend_request_received",
				Payload: notificationData,
			}
			notificationBytes, _ := json.Marshal(notificationMsg)
			wsReceiver.conn.WriteMessage(websocket.TextMessage, notificationBytes)
		}
	}

	return nil
}

func (s *WSServer) handleFriendRequestResponse(wsConn *WSConnection, payload json.RawMessage) error {
	// 解析处理请求
	var handlePayload FriendRequestHandlePayload
	if err := json.Unmarshal(payload, &handlePayload); err != nil {
		return errors.New("无效的请求格式")
	}

	// 获取数据库连接
	db, err := database.GetDB()
	if err != nil {
		return errors.New("数据库连接失败")
	}

	// 验证action
	if handlePayload.Action != "accept" && handlePayload.Action != "reject" {
		return errors.New("无效的操作类型")
	}

	// 获取好友申请记录
	var friendRequest model.FriendRequest
	if err := db.Where("request_id = ?", handlePayload.RequestID).First(&friendRequest).Error; err != nil {
		return errors.New("好友申请不存在")
	}

	// 验证是否是请求的接收者
	if friendRequest.ToUID != wsConn.uid {
		return errors.New("无权处理该请求")
	}

	// 检查请求状态
	if friendRequest.Status != "pending" {
		return errors.New("该请求已被处理")
	}

	// 获取申请者信息用于通知
	var fromUser model.User
	if err := db.Where("uid = ?", friendRequest.FromUID).First(&fromUser).Error; err != nil {
		return errors.New("获取用户信息失败")
	}

	// 获取处理者信息用于通知
	var toUser model.User
	if err := db.Where("uid = ?", wsConn.uid).First(&toUser).Error; err != nil {
		return errors.New("获取用户信息失败")
	}

	// 开启事务
	tx := db.Begin()

	// 更新请求状态
	status := ""
	if handlePayload.Action == "accept" {
		status = "accepted"
	} else if handlePayload.Action == "reject" {
		status = "rejected"
	}
	
	if err := tx.Model(&friendRequest).Updates(map[string]interface{}{
		"status":     status,
		"updated_at": time.Now(),
	}).Error; err != nil {
		tx.Rollback()
		return errors.New("更新请求状态失败")
	}

	var resultMessage string
	if handlePayload.Action == "accept" {
		// 创建双向好友关系
		friends := []model.Friend{
			{
				UserID:    friendRequest.FromUID,
				FriendID:  friendRequest.ToUID,
				CreatedAt: time.Now(),
			},
			{
				UserID:    friendRequest.ToUID,
				FriendID:  friendRequest.FromUID,
				CreatedAt: time.Now(),
			},
		}

		for _, friend := range friends {
			if err := tx.Create(&friend).Error; err != nil {
				tx.Rollback()
				return errors.New("创建好友关系失败")
			}
		}
		resultMessage = "好友请求已接受"
	} else {
		resultMessage = "好友请求已拒绝"
	}

	// 提交事务
	if err := tx.Commit().Error; err != nil {
		return errors.New("处理请求失败")
	}

	// 发送响应给处理者
	response := struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}{
		Success: true,
		Message: resultMessage,
	}
	responseData, _ := json.Marshal(response)
	responseMsg := WSMessage{
		Type:    "friend_request_handle_response",
		Payload: responseData,
	}
	responseBytes, _ := json.Marshal(responseMsg)
	wsConn.conn.WriteMessage(websocket.TextMessage, responseBytes)

	// 发送通知给申请者
	notification := FriendRequestResultNotification{
		RequestID: handlePayload.RequestID,
		FromUID:   wsConn.uid,
		FromName:  toUser.Name,
		Action:    handlePayload.Action,
		Message:   resultMessage,
	}
	notificationData, _ := json.Marshal(notification)
	notificationMsg := WSMessage{
		Type:    "friend_request_result",
		Payload: notificationData,
	}
	notificationBytes, _ := json.Marshal(notificationMsg)

	// 如果申请者在线，发送通知
	if receiverConn, ok := s.connections.Load(friendRequest.FromUID); ok {
		if wsReceiver, ok := receiverConn.(*WSConnection); ok {
			wsReceiver.conn.WriteMessage(websocket.TextMessage, notificationBytes)
		}
	}

	return nil
}

func (s *WSServer) handleGroupJoinRequest(wsConn *WSConnection, payload json.RawMessage) error {
	// 解析请求
	var requestPayload GroupJoinRequestPayload
	if err := json.Unmarshal(payload, &requestPayload); err != nil {
		return errors.New("无效的请求格式")
	}

	// 获取数据库连接
	db, err := database.GetDB()
	if err != nil {
		return errors.New("数据库连接失败")
	}

	// 验证必要字段
	if requestPayload.GroupID == 0 {
		return errors.New("缺少必要字段")
	}

	// 检查群聊是否存在
	var group model.ChatGroup
	if err := db.Where("gid = ?", requestPayload.GroupID).First(&group).Error; err != nil {
		return errors.New("群聊不存在")
	}

	// 检查是否已经是群成员
	var existingMember model.GroupMember
	if err := db.Where("gid = ? AND uid = ?", requestPayload.GroupID, wsConn.uid).First(&existingMember).Error; err == nil {
		return errors.New("已经是群成员")
	}

	// 检查是否有待处理的申请
	var existingRequest model.GroupJoinRequest
	err = db.Where("user_id = ? AND group_id = ? AND status = ?",
		wsConn.uid, requestPayload.GroupID, "pending").First(&existingRequest).Error
	if err == nil {
		return errors.New("已有待处理的入群申请")
	}

	// 检查是否被拒绝且未重新申请
	var rejectedRequest model.GroupJoinRequest
	err = db.Where("user_id = ? AND group_id = ? AND status = ?",
		wsConn.uid, requestPayload.GroupID, "rejected").Order("updated_at desc").First(&rejectedRequest).Error
	if err == nil {
		// 如果有被拒绝的请求，允许重新申请
	}

	// 获取申请者信息
	var fromUser model.User
	if err := db.Where("uid = ?", wsConn.uid).First(&fromUser).Error; err != nil {
		return errors.New("获取用户信息失败")
	}

	// 创建入群申请
	groupJoinReq := model.GroupJoinRequest{
		UserID:    wsConn.uid,
		GroupID:   requestPayload.GroupID,
		Message:   requestPayload.Message,
		Status:    "pending",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := db.Create(&groupJoinReq).Error; err != nil {
		return errors.New("创建入群申请失败")
	}

	// 发送响应给申请者
	response := GroupJoinRequestResponse{
		Success: true,
		Message: "入群申请已发送",
	}
	responseData, _ := json.Marshal(response)
	responseMsg := WSMessage{
		Type:    "group_join_request_response",
		Payload: responseData,
	}
	responseBytes, _ := json.Marshal(responseMsg)
	wsConn.conn.WriteMessage(websocket.TextMessage, responseBytes)

	// 获取群主和管理员列表
	var admins []model.GroupMember
	db.Where("gid = ? AND role IN ('owner', 'admin')", requestPayload.GroupID).Find(&admins)

	// 准备通知消息
	notification := GroupJoinRequestNotification{
		RequestID:    groupJoinReq.RequestID,
		GroupID:      requestPayload.GroupID,
		GroupName:    group.Name,
		FromUID:      wsConn.uid,
		FromName:     fromUser.Name,
		FromAvatar:   fromUser.AvatarURL,
		Message:      requestPayload.Message,
		CreatedAt:    groupJoinReq.CreatedAt.Format("2006-01-02 15:04:05"),
	}
	notificationData, _ := json.Marshal(notification)
	notificationMsg := WSMessage{
		Type:    "group_join_request_received",
		Payload: notificationData,
	}
	notificationBytes, _ := json.Marshal(notificationMsg)

	// 向所有在线的群主和管理员发送通知
	for _, admin := range admins {
		if receiverConn, ok := s.connections.Load(admin.UID); ok {
			if wsReceiver, ok := receiverConn.(*WSConnection); ok {
				wsReceiver.conn.WriteMessage(websocket.TextMessage, notificationBytes)
			}
		}
	}

	return nil
}

func (s *WSServer) handleGroupJoinRequestResponse(wsConn *WSConnection, payload json.RawMessage) error {
	// 解析处理请求
	var handlePayload GroupJoinRequestHandlePayload
	if err := json.Unmarshal(payload, &handlePayload); err != nil {
		return errors.New("无效的请求格式")
	}

	// 获取数据库连接
	db, err := database.GetDB()
	if err != nil {
		return errors.New("数据库连接失败")
	}

	// 验证action
	if handlePayload.Action != "accept" && handlePayload.Action != "reject" {
		return errors.New("无效的操作类型")
	}

	// 获取群聊申请记录
	var groupRequest model.GroupJoinRequest
	if err := db.Where("request_id = ?", handlePayload.RequestID).First(&groupRequest).Error; err != nil {
		return errors.New("入群申请不存在")
	}

	// 获取群聊信息
	var group model.ChatGroup
	if err := db.Where("gid = ?", groupRequest.GroupID).First(&group).Error; err != nil {
		return errors.New("群聊不存在")
	}

	// 检查处理者权限
	var handlerMember model.GroupMember
	if err := db.Where("gid = ? AND uid = ? AND role IN ('owner', 'admin')", 
		groupRequest.GroupID, wsConn.uid).First(&handlerMember).Error; err != nil {
		return errors.New("无权处理该请求")
	}

	// 检查请求状态
	if groupRequest.Status != "pending" {
		return errors.New("该请求已被处理")
	}

	// 获取处理者信息
	var handler model.User
	if err := db.Where("uid = ?", wsConn.uid).First(&handler).Error; err != nil {
		return errors.New("获取处理者信息失败")
	}

	// 开启事务
	tx := db.Begin()

	// 更新请求状态
	if err := tx.Model(&groupRequest).Updates(map[string]interface{}{
		"status":      handlePayload.Action,
		"handler_uid": wsConn.uid,
		"updated_at":  time.Now(),
	}).Error; err != nil {
		tx.Rollback()
		return errors.New("更新请求状态失败")
	}

	var resultMessage string
	if handlePayload.Action == "accept" {
		// 添加为群成员
		newMember := model.GroupMember{
			GID:      groupRequest.GroupID,
			UID:      groupRequest.UserID,
			Role:     "member",
			JoinedAt: time.Now(),
		}
		if err := tx.Create(&newMember).Error; err != nil {
			tx.Rollback()
			return errors.New("添加群成员失败")
		}
		resultMessage = "入群申请已接受"
	} else {
		resultMessage = "入群申请已拒绝"
	}

	// 提交事务
	if err := tx.Commit().Error; err != nil {
		return errors.New("处理请求失败")
	}

	// 发送响应给处理者
	response := struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}{
		Success: true,
		Message: resultMessage,
	}
	responseData, _ := json.Marshal(response)
	responseMsg := WSMessage{
		Type:    "group_join_request_handle_response",
		Payload: responseData,
	}
	responseBytes, _ := json.Marshal(responseMsg)
	wsConn.conn.WriteMessage(websocket.TextMessage, responseBytes)

	// 准备通知消息
	notification := GroupJoinRequestResultNotification{
		RequestID:    handlePayload.RequestID,
		GroupID:      groupRequest.GroupID,
		GroupName:    group.Name,
		HandlerUID:   wsConn.uid,
		HandlerName:  handler.Name,
		HandlerRole:  handlerMember.Role,
		Action:       handlePayload.Action,
		Message:      resultMessage,
	}
	notificationData, _ := json.Marshal(notification)
	notificationMsg := WSMessage{
		Type:    "group_join_request_result",
		Payload: notificationData,
	}
	notificationBytes, _ := json.Marshal(notificationMsg)

	// 通知申请者
	if receiverConn, ok := s.connections.Load(groupRequest.UserID); ok {
		if wsReceiver, ok := receiverConn.(*WSConnection); ok {
			wsReceiver.conn.WriteMessage(websocket.TextMessage, notificationBytes)
		}
	}

	// 获取其他管理员列表（如果处理者是群主，通知所有管理员；如果是管理员，通知群主和其他管理员）
	var otherAdmins []model.GroupMember
	var roles []string
	if handlerMember.Role == "owner" {
		roles = []string{"admin"}
	} else {
		roles = []string{"owner", "admin"}
	}
	query := db.Where("gid = ? AND uid != ? AND role IN ?", 
		groupRequest.GroupID, wsConn.uid, roles)
	query.Find(&otherAdmins)

	// 通知其他管理员
	for _, admin := range otherAdmins {
		if receiverConn, ok := s.connections.Load(admin.UID); ok {
			if wsReceiver, ok := receiverConn.(*WSConnection); ok {
				wsReceiver.conn.WriteMessage(websocket.TextMessage, notificationBytes)
			}
		}
	}

	return nil
}

func (s *WSServer) sendError(wsConn *WSConnection, message string) {
	response := struct {
		Type    string `json:"type"`
		Payload struct {
			Message string `json:"message"`
		} `json:"payload"`
	}{
		Type: "error",
		Payload: struct {
			Message string `json:"message"`
		}{
			Message: message,
		},
	}
	
	if data, err := json.Marshal(response); err == nil {
		wsConn.conn.WriteMessage(websocket.TextMessage, data)
	}
}

// SendMessage 向指定用户发送消息
func (s *WSServer) SendMessage(uid string, message []byte) error {
	if conn, ok := s.connections.Load(uid); ok {
		if wsConn, ok := conn.(*WSConnection); ok {
			return wsConn.conn.WriteMessage(websocket.TextMessage, message)
		}
	}
	return errors.New("用户未连接")
}

// BroadcastMessage 广播消息给所有已认证的用户
func (s *WSServer) BroadcastMessage(message []byte) {
	s.connections.Range(func(key, value interface{}) bool {
		if wsConn, ok := value.(*WSConnection); ok && wsConn.isAuth {
			wsConn.conn.WriteMessage(websocket.TextMessage, message)
		}
		return true
	})
}

// getConversationID 生成会话ID
func getConversationID(uid1, uid2 string) string {
	// 确保会话ID的一致性（两个用户之间的会话ID始终相同）
	if uid1 < uid2 {
		return fmt.Sprintf("%s_%s", uid1, uid2)
	}
	return fmt.Sprintf("%s_%s", uid2, uid1)
}