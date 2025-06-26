package model

import "time"

// AIConversation 对话会话表
type AIConversation struct {
	ConversationID string    `gorm:"column:conversation_id;primary_key" json:"conversation_id"`
	UserID         string    `gorm:"column:user_id" json:"user_id"`
	Title          string    `gorm:"column:title" json:"title"`           // 对话标题
	CreatedAt      time.Time `gorm:"column:created_at" json:"created_at"` // 创建时间
	UpdatedAt      time.Time `gorm:"column:updated_at" json:"updated_at"` // 最后更新时间
}

// AIMessage 对话消息表
type AIMessage struct {
	MessageID      int64     `gorm:"column:message_id;primary_key;auto_increment" json:"message_id"`
	ConversationID string    `gorm:"column:conversation_id" json:"conversation_id"`
	Role           string    `gorm:"column:role" json:"role"`      // user 或 assistant
	Content        string    `gorm:"column:content" json:"content"` // 消息内容
	CreatedAt      time.Time `gorm:"column:created_at" json:"created_at"`
}

func (AIConversation) TableName() string {
	return "ai_conversations"
}

func (AIMessage) TableName() string {
	return "ai_messages"
}

// WebSocketMessage websocket消息结构
type WebSocketMessage struct {
	Type    string      `json:"type"`    // message, start, end, error
	Content string      `json:"content"` // 消息内容
	Data    interface{} `json:"data"`    // 其他数据
}

// AIRequest AI请求结构
type AIRequest struct {
	ConversationID string `json:"conversation_id"` // 为空则创建新对话
	Message        string `json:"message"`         // 用户消息
} 