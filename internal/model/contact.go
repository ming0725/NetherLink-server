package model

// FriendInfo 好友信息
type FriendInfo struct {
	UserID    string `json:"uid"`
	Name      string `json:"name"`
	Avatar    string `json:"avatar_url"`
	Signature string `json:"signature"`
	Status    int    `json:"status"`
}

// GroupMemberInfo 群成员信息
type GroupMemberInfo struct {
	UID      string `json:"uid" gorm:"column:uid"`
	Name     string `json:"name" gorm:"column:name"`
	Avatar   string `json:"avatar_url" gorm:"column:avatar_url"`
	Role     string `json:"role" gorm:"column:role"`
}

// GroupInfo 群组信息
type GroupInfo struct {
	GID     int              `json:"gid"`
	Name    string           `json:"name"`
	Avatar  string           `json:"avatar_url"`
	OwnerID string           `json:"owner_id"`
	Members []GroupMemberInfo `json:"members"`
}

// UserInfo 用户基本信息
type UserInfo struct {
	Name   string `json:"name"`
	Avatar string `json:"avatar_url"`
}

// ContactResponse 联系人信息响应
type ContactResponse struct {
	User    UserInfo     `json:"user"`
	Friends []FriendInfo `json:"friends"`
	Groups  []GroupInfo  `json:"groups"`
} 