package model

import "time"

type Post struct {
	PostID    int64     `gorm:"column:post_id;primary_key;auto_increment" json:"post_id"`
	UserID    string    `gorm:"column:user_id" json:"user_id"`
	Title     string    `gorm:"column:title" json:"title"`
	Content   string    `gorm:"column:content" json:"content"`
	ImageURL  string    `gorm:"column:image_url" json:"image_url"`
	CreatedAt time.Time `gorm:"column:created_at" json:"created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at" json:"updated_at"`
	Likes     []PostLike  `gorm:"foreignKey:PostID" json:"likes"`
	Comments  []Comment   `gorm:"foreignKey:PostID" json:"comments"`
}

type PostLike struct {
	PostID  int64     `gorm:"column:post_id;primaryKey" json:"post_id"`
	UserID  string    `gorm:"column:user_id;primaryKey" json:"user_id"`
	LikedAt time.Time `gorm:"column:liked_at" json:"liked_at"`
}

type Comment struct {
	CommentID int64     `gorm:"column:comment_id;primary_key;auto_increment" json:"comment_id"`
	PostID    int64     `gorm:"column:post_id" json:"post_id"`
	UserID    string    `gorm:"column:user_id" json:"user_id"`
	Content   string    `gorm:"column:content" json:"content"`
	CreatedAt time.Time `gorm:"column:created_at" json:"created_at"`
}

type PostPreview struct {
	PostID      int64   `json:"post_id"`
	Title       string  `json:"title"`
	UserID      string  `json:"user_id"`
	UserName    string  `json:"user_name"`
	UserAvatar  *string `json:"user_avatar"`
	FirstImage  *string `json:"first_image"`
	LikesCount  int64   `json:"likes_count"`
	IsLiked     bool    `json:"is_liked"`
	CreatedAt   string  `json:"created_at"`
}

func (Post) TableName() string {
	return "posts"
}

func (PostLike) TableName() string {
	return "post_likes"
}

func (Comment) TableName() string {
	return "comments"
} 