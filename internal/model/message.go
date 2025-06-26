package model

import (
	"database/sql/driver"
	"encoding/json"
	"time"
)

type MessageType string

const (
	MessageTypeText   MessageType = "text"
	MessageTypeImage  MessageType = "image"
	MessageTypeFile   MessageType = "file"
	MessageTypeEmoji  MessageType = "emoji"
)

type MessageExtra map[string]interface{}

func (e MessageExtra) Value() (driver.Value, error) {
	return json.Marshal(e)
}

func (e *MessageExtra) Scan(value interface{}) error {
	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(bytes, e)
}

type Message struct {
	ID           int64       `gorm:"column:id;primary_key;auto_increment" json:"id"`
	Conversation string      `gorm:"column:conversation" json:"conversation"`
	SenderID     string      `gorm:"column:sender_id" json:"sender_id"`
	Timestamp    time.Time   `gorm:"column:timestamp" json:"timestamp"`
	Type         MessageType `gorm:"column:type" json:"type"`
	Content      string      `gorm:"column:content" json:"content"`
	Extra        MessageExtra `gorm:"column:extra" json:"extra"`
}

func (Message) TableName() string {
	return "message"
} 