package model

import (
	"time"
)

type User struct {
	UID        string    `gorm:"column:uid;primary_key" json:"uid"`
	ID         string    `gorm:"column:id;unique" json:"id"`
	Email      string    `gorm:"column:email;unique" json:"email"`
	Name       string    `gorm:"column:name" json:"name"`
	Password   string    `gorm:"column:password" json:"-"`
	AvatarURL  string    `gorm:"column:avatar_url" json:"avatar_url"`
	Signature  string    `gorm:"column:signature" json:"signature"`
	Status     int       `gorm:"column:status" json:"status"`
	CreatedAt  time.Time `gorm:"column:created_at" json:"created_at"`
	UpdatedAt  time.Time `gorm:"column:updated_at" json:"updated_at"`
}

type Friend struct {
	UserID    string    `gorm:"column:user_id;primary_key" json:"user_id"`
	FriendID  string    `gorm:"column:friend_id;primary_key" json:"friend_id"`
	CreatedAt time.Time `gorm:"column:created_at" json:"created_at"`
}

type ChatGroup struct {
	GID       int       `gorm:"column:gid;primary_key;auto_increment" json:"gid"`
	Name      string    `gorm:"column:name" json:"name"`
	OwnerID   string    `gorm:"column:owner_id" json:"owner_id"`
	Avatar    string    `gorm:"column:avatar" json:"avatar"`
	CreatedAt time.Time `gorm:"column:created_at" json:"created_at"`
}

type GroupMember struct {
	GID      int       `gorm:"column:gid;primary_key" json:"gid"`
	UID      string    `gorm:"column:uid;primary_key" json:"uid"`
	Role     string    `gorm:"column:role;default:member" json:"role"`
	JoinedAt time.Time `gorm:"column:joined_at" json:"joined_at"`
}

type PrivateMessage struct {
	ID         int64     `gorm:"column:id;primary_key;auto_increment" json:"id"`
	ReceiverID string    `gorm:"column:receiver_id" json:"receiver_id"`
	SenderID   string    `gorm:"column:sender_id" json:"sender_id"`
	Timestamp  time.Time `gorm:"column:timestamp" json:"timestamp"`
	Type       string    `gorm:"column:type" json:"type"`
	Content    string    `gorm:"column:content" json:"content"`
	Extra      string    `gorm:"column:extra" json:"extra"`
}

type GroupMessage struct {
	ID        int64     `gorm:"column:id;primary_key;auto_increment" json:"id"`
	GroupID   string    `gorm:"column:group_id" json:"group_id"`
	SenderID  string    `gorm:"column:sender_id" json:"sender_id"`
	Timestamp time.Time `gorm:"column:timestamp" json:"timestamp"`
	Type      string    `gorm:"column:type" json:"type"`
	Content   string    `gorm:"column:content" json:"content"`
	Extra     string    `gorm:"column:extra" json:"extra"`
}

type FriendRequest struct {
	RequestID  int64     `gorm:"column:request_id;primary_key;auto_increment" json:"request_id"`
	FromUID    string    `gorm:"column:from_uid" json:"from_uid"`
	ToUID      string    `gorm:"column:to_uid" json:"to_uid"`
	Message    string    `gorm:"column:message" json:"message"`
	Status     string    `gorm:"column:status;default:pending" json:"status"`
	CreatedAt  time.Time `gorm:"column:created_at" json:"created_at"`
	UpdatedAt  time.Time `gorm:"column:updated_at" json:"updated_at"`
}

type GroupJoinRequest struct {
	RequestID  int64     `gorm:"column:request_id;primary_key;auto_increment" json:"request_id"`
	UserID     string    `gorm:"column:user_id" json:"user_id"`
	GroupID    int       `gorm:"column:group_id" json:"group_id"`
	Message    string    `gorm:"column:message" json:"message"`
	Status     string    `gorm:"column:status;default:pending" json:"status"`
	HandlerUID string    `gorm:"column:handler_uid" json:"handler_uid"`
	CreatedAt  time.Time `gorm:"column:created_at" json:"created_at"`
	UpdatedAt  time.Time `gorm:"column:updated_at" json:"updated_at"`
}

func (User) TableName() string {
	return "users"
}

func (Friend) TableName() string {
	return "friends"
}

func (ChatGroup) TableName() string {
	return "chat_groups"
}

func (GroupMember) TableName() string {
	return "group_members"
}

func (PrivateMessage) TableName() string {
	return "private_messages"
}

func (GroupMessage) TableName() string {
	return "group_message"
}

func (FriendRequest) TableName() string {
	return "friend_requests"
}

func (GroupJoinRequest) TableName() string {
	return "group_join_requests"
} 