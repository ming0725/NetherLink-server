package server

import (
	"NetherLink-server/config"
	"NetherLink-server/internal/model"
	"NetherLink-server/pkg/database"
	"NetherLink-server/pkg/utils"
	"bufio"
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"gorm.io/gorm"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	codeStore      = make(map[string]codeEntry)
	codeStoreMutex sync.Mutex
)

type codeEntry struct {
	Code      string
	ExpiresAt time.Time
}

type sendCodeRequest struct {
	Email string `json:"email" binding:"required,email"`
}

type registerRequest struct {
	Email      string `json:"email" binding:"required,email"`
	User       string `json:"user" binding:"required"`
	Passwd     string `json:"passwd" binding:"required"`
	VarifyCode string `json:"varifycode" binding:"required"`
	AvatarURL  string `json:"avatar_url"`
}

type loginRequest struct {
	Email  string `json:"email" binding:"required,email"`
	Passwd string `json:"passwd" binding:"required"`
}

type loginResponse struct {
	UID       string `json:"uid"`
	User      string `json:"user"`
	Email     string `json:"email"`
	AvatarURL string `json:"avatar_url"`
	Token     string `json:"token"`
}

type contactRequest struct {
	UID   string `json:"uid" binding:"required"`
	Token string `json:"token" binding:"required"`
}

type createPostRequest struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}

type createCommentRequest struct {
	Content string `json:"content" binding:"required"`
}

// SearchUserResponse 用户搜索响应结构
type SearchUserResponse struct {
	UID       string `json:"uid"`
	ID        string `json:"id"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
	Signature string `json:"signature"`
	Status    int    `json:"status"`
}

// SearchGroupResponse 群聊搜索响应结构
type SearchGroupResponse struct {
	GID         int    `json:"gid"`
	Name        string `json:"name"`
	Avatar      string `json:"avatar"`
	MemberCount int    `json:"member_count"`
}

type HTTPServer struct {
	engine *gin.Engine
}

// AIHandler 处理AI对话的WebSocket连接
type AIHandler struct {
	conn        *websocket.Conn
	userID      string
	isStreaming bool
	streamLock  sync.Mutex
	activeConv  string
}

// NewAIHandler 创建新的AI处理器
func NewAIHandler(conn *websocket.Conn, userID string) *AIHandler {
	return &AIHandler{
		conn:   conn,
		userID: userID,
	}
}

func NewHTTPServer(engine *gin.Engine) *HTTPServer {
	server := &HTTPServer{
		engine: engine,
	}
	server.setupRoutes()
	return server
}

func (s *HTTPServer) setupRoutes() {
	s.engine.POST("/api/send_code", sendCodeHandler)
	s.engine.Static(config.GlobalConfig.Image.URLPrefix, config.GlobalConfig.Image.UploadDir)
	s.engine.Static("/uploads/posts", "uploads/posts")
	s.engine.GET("/favicon.ico", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	s.engine.POST("/api/upload_image", uploadImageHandler)
	s.engine.POST("/api/register", registerHandler)
	s.engine.POST("/api/login", loginHandler)
	s.engine.GET("/api/contacts", authMiddleware(), getContactsHandler)
	s.engine.GET("/api/search/users", authMiddleware(), searchUsersHandler)
	s.engine.GET("/api/search/groups", authMiddleware(), searchGroupsHandler)
	s.engine.GET("/api/posts", authMiddleware(), getPostsHandler)
	s.engine.POST("/api/posts", authMiddleware(), createPostHandler)
	s.engine.GET("/api/posts/:post_id", authMiddleware(), getPostDetailHandler)
	s.engine.POST("/api/posts/:post_id/comments", authMiddleware(), createCommentHandler)
	s.engine.POST("/api/posts/:post_id/like", authMiddleware(), togglePostLikeHandler)
	s.engine.GET("/ws/ai", authMiddleware(), s.handleAIWebSocket)
}

func sendCodeHandler(c *gin.Context) {
	var req sendCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}

	code := generateCode(6)
	expiresAt := time.Now().Add(3 * time.Minute)

	codeStoreMutex.Lock()
	codeStore[req.Email] = codeEntry{Code: code, ExpiresAt: expiresAt}
	codeStoreMutex.Unlock()

	emailCfg := config.GlobalConfig.Email
	sender := utils.NewEmailSender(emailCfg.SMTPHost, emailCfg.SMTPPort, emailCfg.Sender, emailCfg.DisplayName, emailCfg.Password, emailCfg.UseSSL)

	template := utils.GetEmailTemplate(code)
	err := sender.Send(req.Email, "NetherLink 注册验证码", template)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "邮件发送失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "验证码已发送"})
}

func generateCode(length int) string {
	rand.Seed(time.Now().UnixNano())
	min := int64(100000)
	max := int64(999999)
	return fmt.Sprintf("%06d", rand.Int63n(max-min+1)+min)
}

func uploadImageHandler(c *gin.Context) {
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "未找到文件"})
		return
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext != ".jpg" && ext != ".jpeg" && ext != ".png" && ext != ".gif" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "仅支持jpg、jpeg、png、gif格式"})
		return
	}

	filename := generateImageFilename(ext)
	savePath := utils.GetImageSavePath(filename)

	// 确保目录存在
	if err := os.MkdirAll(filepath.Dir(savePath), 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建目录失败"})
		return
	}

	out, err := os.Create(savePath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存文件失败"})
		return
	}
	defer out.Close()

	if _, err := io.Copy(out, file); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "写入文件失败"})
		return
	}

	url := utils.GetFullImageURL(filename)
	c.JSON(http.StatusOK, gin.H{"url": url})
}

func generateImageFilename(ext string) string {
	t := time.Now().UnixNano()
	r := rand.Intn(10000)
	return fmt.Sprintf("img_%d_%d%s", t, r, ext)
}

func registerHandler(c *gin.Context) {
	var req registerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}

	// 校验验证码
	codeStoreMutex.Lock()
	entry, ok := codeStore[req.Email]
	codeStoreMutex.Unlock()
	if !ok || entry.Code != req.VarifyCode || time.Now().After(entry.ExpiresAt) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "验证码无效或已过期"})
		return
	}

	db, err := getDB()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "数据库连接失败"})
		return
	}

	// 检查邮箱是否已存在
	var count int64
	db.Model(&model.User{}).Where("email = ?", req.Email).Count(&count)
	if count > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "邮箱已存在"})
		return
	}

	// md5加密密码
	h := md5.New()
	h.Write([]byte(req.Passwd))
	passwdHash := hex.EncodeToString(h.Sum(nil))

	// 生成UUID并获取前8位作为用户ID
	fullUUID := generateUUID()
	userID := fullUUID[:8]

	user := model.User{
		UID:       fullUUID,
		ID:        userID, // 使用UUID前8位作为用户ID
		Email:     req.Email,
		Name:      req.User, // 使用用户输入的名称作为显示名
		Password:  passwdHash,
		AvatarURL: req.AvatarURL,
		Status:    0, // 默认状态
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := db.Create(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建用户失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"uid":        user.UID,
		"user":       user.ID, // 返回8位用户ID
		"email":      user.Email,
		"avatar_url": user.AvatarURL,
	})
}

func loginHandler(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}

	db, err := getDB()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "数据库连接失败"})
		return
	}

	// md5加密密码
	h := md5.New()
	h.Write([]byte(req.Passwd))
	passwdHash := hex.EncodeToString(h.Sum(nil))

	var user model.User
	if err := db.Where("email = ? AND password = ?", req.Email, passwdHash).First(&user).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "邮箱或密码错误"})
		return
	}

	token, err := generateJWT(user.UID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "生成token失败"})
		return
	}

	c.JSON(http.StatusOK, loginResponse{
		UID:       user.UID,
		User:      user.ID,
		Email:     user.Email,
		AvatarURL: user.AvatarURL,
		Token:     token,
	})
}

func generateJWT(uid string) (string, error) {
	claims := jwt.MapClaims{
		"uid": uid,
		"iss": "netherlink",
		"exp": time.Now().Add(config.GlobalConfig.JWT.Expire).Unix(),
	}
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return t.SignedString([]byte(config.GlobalConfig.JWT.Secret))
}

// getDB 获取gorm.DB实例
func getDB() (*gorm.DB, error) {
	return database.GetDB()
}

// generateUUID 生成8位用户ID，基于UUID前8位，处理冲突
func generateUUID() string {
	// 生成完整的UUID
	fullUUID := uuid.New().String()
	// 获取前8位
	shortID := fullUUID[:8]

	// 获取数据库连接
	db, err := getDB()
	if err != nil {
		// 如果无法连接数据库，返回完整UUID作为后备方案
		return fullUUID
	}

	// 检查是否存在冲突
	for {
		var count int64
		if err := db.Model(&model.User{}).Where("uid LIKE ?", shortID+"%").Count(&count).Error; err != nil {
			return fullUUID // 数据库错误时返回完整UUID
		}

		if count == 0 {
			return fullUUID // 返回完整UUID，但前8位是我们要用的ID
		}

		// 将shortID视为16进制数，加1后继续尝试
		if num, err := strconv.ParseUint(shortID, 16, 32); err == nil {
			num++
			shortID = fmt.Sprintf("%08x", num)
		} else {
			return fullUUID // 转换失败时返回完整UUID
		}
	}
}

// authMiddleware JWT认证中间件
func authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"code": -1, "message": "未提供认证信息"})
			c.Abort()
			return
		}

		// 检查Authorization格式
		parts := strings.SplitN(authHeader, " ", 2)
		if !(len(parts) == 2 && parts[0] == "Bearer") {
			c.JSON(http.StatusUnauthorized, gin.H{"code": -1, "message": "认证格式错误"})
			c.Abort()
			return
		}

		// 解析JWT token
		claims := jwt.MapClaims{}
		token, err := jwt.ParseWithClaims(parts[1], claims, func(token *jwt.Token) (interface{}, error) {
			return []byte(config.GlobalConfig.JWT.Secret), nil
		})

		if err != nil || !token.Valid {
			c.JSON(http.StatusUnauthorized, gin.H{"code": -1, "message": "无效的token"})
			c.Abort()
			return
		}

		// 将用户信息存储到上下文
		uid, ok := claims["uid"].(string)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"code": -1, "message": "无效的token格式"})
			c.Abort()
			return
		}

		c.Set("user_id", uid)
		c.Next()
	}
}

func getContactsHandler(c *gin.Context) {
	// 从上下文获取uid（已经在中间件中验证）
	uid, _ := c.Get("user_id")
	userID := uid.(string)

	db, err := getDB()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": -1, "message": "数据库连接失败"})
		return
	}

	response := model.ContactResponse{}

	// 获取用户自己的信息
	var userResult struct {
		Name      string `gorm:"column:name"`
		AvatarURL string `gorm:"column:avatar_url"`
	}
	if err := db.Table("users").
		Select("name, avatar_url").
		Where("uid = ?", userID).
		First(&userResult).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": -1, "message": "获取用户信息失败"})
		return
	}

	response.User = model.UserInfo{
		Name:   userResult.Name,
		Avatar: userResult.AvatarURL,
	}

	// 获取好友信息
	type FriendResult struct {
		UserID    string `gorm:"column:uid"`
		Name      string `gorm:"column:name"`
		AvatarURL string `gorm:"column:avatar_url"`
		Signature string `gorm:"column:signature"`
		Status    int    `gorm:"column:status"`
	}

	var friends []FriendResult
	if err := db.Table("users").
		Select("users.uid, users.name, users.avatar_url, users.signature, users.status").
		Joins("INNER JOIN friends ON users.uid = friends.friend_id").
		Where("friends.user_id = ?", userID).
		Find(&friends).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取好友列表失败"})
		return
	}

	// 转换好友信息
	for _, f := range friends {
		response.Friends = append(response.Friends, model.FriendInfo{
			UserID:    f.UserID,
			Name:      f.Name,
			Avatar:    f.AvatarURL,
			Signature: f.Signature,
			Status:    f.Status,
		})
	}

	// 获取群组信息
	type GroupResult struct {
		GID     int    `gorm:"column:gid"`
		Name    string `gorm:"column:name"`
		Avatar  string `gorm:"column:avatar"`
		OwnerID string `gorm:"column:owner_id"`
	}

	var groupResults []GroupResult
	if err := db.Table("group_members").
		Select("chat_groups.gid, chat_groups.name, chat_groups.avatar, chat_groups.owner_id").
		Joins("LEFT JOIN chat_groups ON group_members.gid = chat_groups.gid").
		Where("group_members.uid = ?", userID).
		Find(&groupResults).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取群组列表失败"})
		return
	}

	for _, gr := range groupResults {
		// 获取群成员信息
		var members []model.GroupMemberInfo
		if err := db.Table("group_members").
			Select("users.uid, users.name, users.avatar_url as avatar, group_members.role").
			Joins("LEFT JOIN users ON group_members.uid = users.uid").
			Where("group_members.gid = ?", gr.GID).
			Find(&members).Error; err != nil {
			continue
		}

		response.Groups = append(response.Groups, model.GroupInfo{
			GID:     gr.GID,
			Name:    gr.Name,
			Avatar:  gr.Avatar,
			OwnerID: gr.OwnerID,
			Members: members,
		})
	}

	c.JSON(http.StatusOK, response)
}

func getPostsHandler(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"code": -1, "message": "未授权"})
		return
	}

	db, err := getDB()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": -1, "message": "数据库连接失败"})
		return
	}

	var posts []struct {
		PostID     int64     `gorm:"column:post_id"`
		Title      string    `gorm:"column:title"`
		UserID     string    `gorm:"column:user_id"`
		UserName   string    `gorm:"column:name"`
		UserAvatar string    `gorm:"column:avatar_url"`
		ImageURL   string    `gorm:"column:image_url"`
		CreatedAt  time.Time `gorm:"column:created_at"`
	}

	// 联表查询帖子和用户信息
	err = db.Table("posts").
		Select("posts.post_id, posts.title, posts.user_id, posts.image_url, users.name, users.avatar_url, posts.created_at").
		Joins("LEFT JOIN users ON posts.user_id = users.uid").
		Order("posts.created_at DESC").
		Find(&posts).Error

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": -1, "message": "获取帖子列表失败"})
		return
	}

	// 获取每个帖子的点赞信息
	var result []model.PostPreview
	for _, post := range posts {
		var preview model.PostPreview
		preview.PostID = post.PostID
		preview.Title = post.Title
		preview.UserID = post.UserID
		preview.UserName = post.UserName
		preview.UserAvatar = &post.UserAvatar
		preview.CreatedAt = post.CreatedAt.Format("2006-01-02 15:04:05")
		if post.ImageURL != "" {
			preview.FirstImage = &post.ImageURL
		}

		// 获取点赞数
		var likesCount int64
		if err := db.Model(&model.PostLike{}).
			Where("post_id = ?", post.PostID).
			Count(&likesCount).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": -1, "message": "获取点赞数失败"})
			return
		}
		preview.LikesCount = likesCount

		// 检查当前用户是否点赞
		var likeCount int64
		if err := db.Model(&model.PostLike{}).
			Where("post_id = ? AND user_id = ?", post.PostID, userID).
			Count(&likeCount).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": -1, "message": "检查点赞状态失败"})
			return
		}
		preview.IsLiked = likeCount > 0

		result = append(result, preview)
	}

	c.JSON(http.StatusOK, gin.H{
		"posts": result,
	})
}

func createPostHandler(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"code": -1, "message": "未授权"})
		return
	}

	// 获取表单数据
	form, err := c.MultipartForm()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "message": "无效的请求格式"})
		return
	}

	// 从表单中获取 JSON 数据
	jsonData := form.Value["data"]
	if len(jsonData) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "message": "缺少帖子数据"})
		return
	}

	var req createPostRequest
	if err := json.Unmarshal([]byte(jsonData[0]), &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "message": "请求数据格式错误"})
		return
	}

	if req.Title == "" || req.Content == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "message": "标题和内容不能为空"})
		return
	}

	// 获取数据库连接
	db, err := getDB()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": -1, "message": "数据库连接失败"})
		return
	}

	// 创建帖子记录（先不设置图片URL）
	post := &model.Post{
		UserID:    userID,
		Title:     req.Title,
		Content:   req.Content,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := db.Create(post).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": -1, "message": "创建帖子失败"})
		return
	}

	// 处理上传的图片
	files := form.File["images"]
	if len(files) > 0 { // 有上传图片
		file := files[0] // 只处理第一张图片

		// 检查文件类型
		contentType := file.Header.Get("Content-Type")
		if !strings.HasPrefix(contentType, "image/") {
			c.JSON(http.StatusBadRequest, gin.H{"code": -1, "message": "只能上传图片文件"})
			return
		}

		// 生成文件名和保存路径
		ext := filepath.Ext(file.Filename)
		filename := fmt.Sprintf("post_%d%s", post.PostID, ext)
		savePath := filepath.Join("uploads/posts", filename)

		// 保存文件
		if err := c.SaveUploadedFile(file, savePath); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": -1, "message": "保存图片失败"})
			return
		}

		// 更新帖子的图片URL
		imageURL := fmt.Sprintf("%s/uploads/posts/%s", config.GlobalConfig.Server.HTTP.BaseURL, filename)
		if err := db.Model(post).Update("image_url", imageURL).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": -1, "message": "更新图片URL失败"})
			return
		}
		post.ImageURL = imageURL
	}

	// 查询完整的帖子信息
	var postInfo struct {
		PostID     int64     `gorm:"column:post_id"`
		Title      string    `gorm:"column:title"`
		Content    string    `gorm:"column:content"`
		ImageURL   string    `gorm:"column:image_url"`
		UserID     string    `gorm:"column:user_id"`
		UserName   string    `gorm:"column:name"`
		UserAvatar string    `gorm:"column:avatar_url"`
		CreatedAt  time.Time `gorm:"column:created_at"`
	}

	err = db.Table("posts").
		Select("posts.post_id, posts.title, posts.content, posts.image_url, posts.user_id, users.name, users.avatar_url, posts.created_at").
		Joins("LEFT JOIN users ON posts.user_id = users.uid").
		Where("posts.post_id = ?", post.PostID).
		First(&postInfo).Error

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": -1, "message": "获取帖子信息失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"post_id":     postInfo.PostID,
			"title":       postInfo.Title,
			"content":     postInfo.Content,
			"user_id":     postInfo.UserID,
			"user_name":   postInfo.UserName,
			"user_avatar": postInfo.UserAvatar,
			"image_url":   postInfo.ImageURL,
			"created_at":  postInfo.CreatedAt.Format("2006-01-02 15:04:05"),
		},
	})
}

func getPostDetailHandler(c *gin.Context) {
	// 1. 获取帖子ID
	postID, err := strconv.ParseInt(c.Param("post_id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的帖子ID"})
		return
	}

	// 2. 获取当前用户ID
	userID := c.GetString("user_id")

	// 3. 获取数据库连接
	db, err := getDB()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "数据库连接失败"})
		return
	}

	// 4. 查询帖子信息
	var post model.Post
	if err := db.Preload("Comments").Preload("Likes").First(&post, postID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "帖子不存在"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "查询帖子失败"})
		}
		return
	}

	// 5. 查询作者信息
	var author model.User
	if err := db.First(&author, "uid = ?", post.UserID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询作者信息失败"})
		return
	}

	// 6. 检查当前用户是否点赞
	isLiked := false
	for _, like := range post.Likes {
		if like.UserID == userID {
			isLiked = true
			break
		}
	}

	// 7. 查询所有评论用户的信息
	var commentUsers = make(map[string]model.User)
	var userIDs []string
	for _, comment := range post.Comments {
		userIDs = append(userIDs, comment.UserID)
	}
	if len(userIDs) > 0 {
		var users []model.User
		if err := db.Where("uid IN ?", userIDs).Find(&users).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "查询评论用户信息失败"})
			return
		}
		for _, user := range users {
			commentUsers[user.UID] = user
		}
	}

	// 8. 构造带用户信息的评论
	var commentsWithUser []gin.H
	for _, comment := range post.Comments {
		user, exists := commentUsers[comment.UserID]
		commentData := gin.H{
			"comment_id": comment.CommentID,
			"post_id":    comment.PostID,
			"user_id":    comment.UserID,
			"content":    comment.Content,
			"created_at": comment.CreatedAt.Format("2006-01-02 15:04:05"),
		}
		if exists {
			commentData["user_name"] = user.Name
			commentData["user_avatar"] = user.AvatarURL
		} else {
			commentData["user_name"] = "未知用户"
			commentData["user_avatar"] = ""
		}
		commentsWithUser = append(commentsWithUser, commentData)
	}

	// 9. 构造响应
	response := gin.H{
		"post_id": post.PostID,
		"title":   post.Title,
		"content": post.Content,
		"author": gin.H{
			"user_id":   author.UID,
			"user_name": author.Name,
			"avatar":    author.AvatarURL,
		},
		"is_liked":    isLiked,
		"likes_count": len(post.Likes),
		"comments":    commentsWithUser,
	}

	c.JSON(http.StatusOK, response)
}

func createCommentHandler(c *gin.Context) {
	// 1. 获取帖子ID
	postID, err := strconv.ParseInt(c.Param("post_id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的帖子ID"})
		return
	}

	// 2. 获取当前用户ID
	userID := c.GetString("user_id")

	// 3. 解析请求体
	var req createCommentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的请求参数"})
		return
	}

	// 4. 验证评论内容
	if strings.TrimSpace(req.Content) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "评论内容不能为空"})
		return
	}

	// 5. 获取数据库连接
	db, err := getDB()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "数据库连接失败"})
		return
	}

	// 6. 检查帖子是否存在
	var post model.Post
	if err := db.First(&post, postID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "帖子不存在"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "查询帖子失败"})
		}
		return
	}

	// 7. 创建评论
	comment := model.Comment{
		PostID:    postID,
		UserID:    userID,
		Content:   req.Content,
		CreatedAt: time.Now(),
	}

	if err := db.Create(&comment).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建评论失败"})
		return
	}

	// 8. 查询评论者信息
	var user model.User
	if err := db.First(&user, "uid = ?", userID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询用户信息失败"})
		return
	}

	// 9. 返回评论信息
	c.JSON(http.StatusOK, gin.H{
		"comment_id":  comment.CommentID,
		"post_id":     comment.PostID,
		"user_id":     comment.UserID,
		"user_name":   user.Name,
		"user_avatar": user.AvatarURL,
		"content":     comment.Content,
		"created_at":  comment.CreatedAt.Format("2006-01-02 15:04:05"),
	})
}

func togglePostLikeHandler(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"code": -1, "message": "未授权"})
		return
	}

	// 获取帖子ID
	postID, err := strconv.ParseInt(c.Param("post_id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "message": "无效的帖子ID"})
		return
	}

	// 获取数据库连接
	db, err := getDB()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": -1, "message": "数据库连接失败"})
		return
	}

	// 检查帖子是否存在
	var post model.Post
	if err := db.First(&post, postID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"code": -1, "message": "帖子不存在"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"code": -1, "message": "查询帖子失败"})
		}
		return
	}

	// 开启事务
	tx := db.Begin()
	if tx.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": -1, "message": "开启事务失败"})
		return
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// 检查是否已点赞
	var like model.PostLike
	err = tx.Where("post_id = ? AND user_id = ?", postID, userID).First(&like).Error
	wasLiked := err != gorm.ErrRecordNotFound

	if !wasLiked {
		// 未点赞，创建点赞记录
		like = model.PostLike{
			PostID:  postID,
			UserID:  userID,
			LikedAt: time.Now(),
		}
		if err := tx.Create(&like).Error; err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"code": -1, "message": "创建点赞记录失败"})
			return
		}
	} else {
		// 已点赞，删除点赞记录
		if err := tx.Delete(&like).Error; err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"code": -1, "message": "删除点赞记录失败"})
			return
		}
	}

	// 提交事务
	if err := tx.Commit().Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": -1, "message": "提交事务失败"})
		return
	}

	// 获取最新点赞数
	var likesCount int64
	if err := db.Model(&model.PostLike{}).Where("post_id = ?", postID).Count(&likesCount).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": -1, "message": "获取点赞数失败"})
		return
	}

	// 返回最新状态
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"is_liked":    !wasLiked, // 如果原来未点赞，现在就是点赞了；反之亦然
			"likes_count": likesCount,
		},
	})
}

// handleAIWebSocket 处理AI对话的WebSocket连接
func (s *HTTPServer) handleAIWebSocket(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未授权"})
		return
	}

	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			return true // 允许所有来源，生产环境应该配置具体的域名
		},
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("WebSocket升级失败: %v", err)
		return
	}

	handler := NewAIHandler(conn, userID)
	defer handler.Close()

	for {
		messageType, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket错误: %v", err)
			}
			break
		}

		if err := handler.HandleMessage(messageType, message); err != nil {
			log.Printf("处理消息错误: %v", err)
			handler.sendError(err.Error())
		}
	}
}

// HandleMessage 处理接收到的消息
func (h *AIHandler) HandleMessage(messageType int, message []byte) error {
	// 获取流式传输锁
	h.streamLock.Lock()
	if h.isStreaming {
		h.streamLock.Unlock()
		return h.sendError("当前正在处理其他请求，请稍后再试")
	}
	h.isStreaming = true
	h.streamLock.Unlock()

	// 函数结束时清理状态
	defer func() {
		h.streamLock.Lock()
		h.isStreaming = false
		h.streamLock.Unlock()
	}()

	// 解析请求
	var req model.AIRequest
	if err := json.Unmarshal(message, &req); err != nil {
		return h.sendError("无效的请求格式")
	}

	// 处理新对话请求
	if req.ConversationID == "" {
		// 如果存在未完成的对话，先清理
		if h.activeConv != "" {
			if err := h.cleanupIncompleteConversation(h.activeConv); err != nil {
				log.Printf("清理未完成对话失败: %v", err)
			}
		}

		// 创建新对话
		conversationID := uuid.New().String()
		conversation := model.AIConversation{
			ConversationID: conversationID,
			UserID:         h.userID,
			Title:          req.Message[:min(30, len(req.Message))] + "...",
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}

		db, err := getDB()
		if err != nil {
			return h.sendError("数据库连接失败")
		}

		if err := db.Create(&conversation).Error; err != nil {
			return h.sendError("创建对话失败")
		}

		h.activeConv = conversationID
		req.ConversationID = conversationID
	} else {
		// 验证对话所有权
		db, err := getDB()
		if err != nil {
			return h.sendError("数据库连接失败")
		}

		var conv model.AIConversation
		if err := db.Where("conversation_id = ? AND user_id = ?", req.ConversationID, h.userID).First(&conv).Error; err != nil {
			return h.sendError("无效的对话ID或无权访问")
		}

		h.activeConv = req.ConversationID
	}

	// 获取数据库连接
	db, err := getDB()
	if err != nil {
		return h.sendError("数据库连接失败")
	}

	// 保存用户消息
	userMessage := model.AIMessage{
		ConversationID: req.ConversationID,
		Role:           "user",
		Content:        req.Message,
		CreatedAt:      time.Now(),
	}

	if err := db.Create(&userMessage).Error; err != nil {
		return h.sendError("保存消息失败")
	}

	// 获取历史消息
	var messages []model.AIMessage
	if err := db.Where("conversation_id = ?", req.ConversationID).
		Order("created_at ASC"). // 改为正序，保证消息顺序正确
		Limit(config.GlobalConfig.AI.MaxHistory).
		Find(&messages).Error; err != nil {
		return h.sendError("获取历史消息失败")
	}

	// 发送开始标志
	startMsg := model.WebSocketMessage{
		Type: "start",
		Data: map[string]string{
			"conversation_id": req.ConversationID,
		},
	}
	if err := h.sendJSON(startMsg); err != nil {
		return err
	}

	// 构造Deepseek API请求
	type Message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}

	type DeepseekRequest struct {
		Model       string    `json:"model"`
		Messages    []Message `json:"messages"`
		Stream      bool      `json:"stream"`
		Temperature float64   `json:"temperature"`
		MaxTokens   int       `json:"max_tokens"`
	}

	// 构建消息历史
	var apiMessages []Message
	for _, msg := range messages {
		apiMessages = append(apiMessages, Message{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}
	// 添加当前消息
	apiMessages = append(apiMessages, Message{
		Role:    "user",
		Content: req.Message,
	})

	// 准备API请求
	apiReq := DeepseekRequest{
		Model:       config.GlobalConfig.AI.Model,
		Messages:    apiMessages,
		Stream:      true,
		Temperature: 0.7,
		MaxTokens:   2000,
	}

	// 创建HTTP请求
	jsonData, err := json.Marshal(apiReq)
	if err != nil {
		return h.sendError("构造API请求失败")
	}

	httpReq, err := http.NewRequest("POST", config.GlobalConfig.AI.BaseURL+"/v1/chat/completions", bytes.NewBuffer(jsonData))
	if err != nil {
		return h.sendError("创建HTTP请求失败")
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+config.GlobalConfig.AI.APIKey)

	// 发送请求
	client := &http.Client{}
	resp, err := client.Do(httpReq)
	if err != nil {
		return h.sendError("调用AI API失败")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return h.sendError(fmt.Sprintf("AI API返回错误: %s", string(body)))
	}

	// 读取流式响应
	reader := bufio.NewReader(resp.Body)
	var fullResponse strings.Builder

	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			return h.sendError("读取API响应失败")
		}

		// 跳过空行
		if len(line) <= 1 {
			continue
		}

		// 解析SSE数据
		data := strings.TrimPrefix(string(line), "data: ")
		if data == "[DONE]" {
			break
		}

		var streamResp struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}

		if err := json.Unmarshal([]byte(data), &streamResp); err != nil {
			continue
		}

		if len(streamResp.Choices) > 0 && streamResp.Choices[0].Delta.Content != "" {
			content := streamResp.Choices[0].Delta.Content
			fullResponse.WriteString(content)

			// 发送流式响应
			msg := model.WebSocketMessage{
				Type:    "message",
				Content: content,
			}
			if err := h.sendJSON(msg); err != nil {
				return err
			}
		}
	}

	// 保存AI响应
	aiMessage := model.AIMessage{
		ConversationID: req.ConversationID,
		Role:           "assistant",
		Content:        fullResponse.String(),
		CreatedAt:      time.Now(),
	}

	// 重新获取数据库连接（因为可能已经超时）
	db, err = getDB()
	if err != nil {
		return h.sendError("数据库连接失败")
	}

	if err := db.Create(&aiMessage).Error; err != nil {
		return h.sendError("保存AI响应失败")
	}

	// 更新对话时间
	if err := db.Model(&model.AIConversation{}).
		Where("conversation_id = ?", req.ConversationID).
		Update("updated_at", time.Now()).Error; err != nil {
		log.Printf("更新对话时间失败: %v", err)
	}

	// 发送结束标志
	endMsg := model.WebSocketMessage{
		Type: "end",
	}
	return h.sendJSON(endMsg)
}

// cleanupIncompleteConversation 清理未完成的对话
func (h *AIHandler) cleanupIncompleteConversation(conversationID string) error {
	db, err := getDB()
	if err != nil {
		return err
	}

	// 标记对话为已完成或添加结束标记
	if err := db.Model(&model.AIConversation{}).
		Where("conversation_id = ?", conversationID).
		Update("updated_at", time.Now()).Error; err != nil {
		return err
	}

	return nil
}

// Close 关闭处理器
func (h *AIHandler) Close() {
	// 清理当前活动的对话
	if h.activeConv != "" {
		if err := h.cleanupIncompleteConversation(h.activeConv); err != nil {
			log.Printf("关闭时清理对话失败: %v", err)
		}
	}
}

// sendJSON 发送JSON消息
func (h *AIHandler) sendJSON(message interface{}) error {
	data, err := json.Marshal(message)
	if err != nil {
		return err
	}
	return h.conn.WriteMessage(websocket.TextMessage, data)
}

// sendError 发送错误消息
func (h *AIHandler) sendError(message string) error {
	errMsg := model.WebSocketMessage{
		Type:    "error",
		Content: message,
	}
	return h.sendJSON(errMsg)
}

func searchUsersHandler(c *gin.Context) {
	keyword := c.Query("keyword")
	if keyword == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "搜索关键词不能为空"})
		return
	}

	currentUID := c.GetString("user_id")
	db, err := getDB()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "数据库连接失败"})
		return
	}

	var users []SearchUserResponse

	scoreQuery := `
    CASE 
        WHEN id = ? THEN 100
        WHEN name = ? THEN 90
        WHEN id LIKE ? THEN 80
        WHEN name LIKE ? THEN 70
        WHEN id LIKE ? THEN 60
        WHEN name LIKE ? THEN 50
        ELSE 0
    END AS relevance_score`

	if err := db.Table("users u").
		Select("u.uid, u.id, u.name, u.avatar_url, u.signature, u.status, "+scoreQuery,
			keyword, keyword,
			keyword+"%", keyword+"%",
			"%"+keyword+"%", "%"+keyword+"%",
		).
		Joins("LEFT JOIN friends f ON f.friend_id = u.uid AND f.user_id = ?", currentUID).
		Where("u.uid != ?", currentUID). // 排除自己
		Where("f.friend_id IS NULL").    // 排除已添加的好友
		Where(db.Where("u.id LIKE ?", "%"+keyword+"%").
			Or("u.name LIKE ?", "%"+keyword+"%"),
		).
		Order("relevance_score DESC").
		Limit(20).
		Find(&users).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "搜索用户失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"users": users})
}

func searchGroupsHandler(c *gin.Context) {
	// 获取搜索关键词
	keyword := c.Query("keyword")
	if keyword == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "搜索关键词不能为空",
		})
		return
	}

	db, err := getDB()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "数据库连接失败",
		})
		return
	}

	var groups []SearchGroupResponse

	// 构建子查询来计算每个群的成员数
	memberCountSubQuery := db.Table("group_members").
		Select("gid, COUNT(*) as member_count").
		Group("gid")

	// 构建基础查询
	query := db.Table("chat_groups").
		Select("chat_groups.gid, chat_groups.name, chat_groups.avatar, IFNULL(member_counts.member_count, 0) as member_count").
		Joins("LEFT JOIN (?) as member_counts ON chat_groups.gid = member_counts.gid", memberCountSubQuery)

	// 使用CASE WHEN来计算相关度得分
	scoreQuery := `
		CASE 
			WHEN name = ? THEN 100    -- 完全匹配群名
			WHEN name LIKE ? THEN 80   -- 群名前缀匹配
			WHEN name LIKE ? THEN 60   -- 群名包含关键词
			ELSE 0
		END as relevance_score`

	// 构建搜索条件
	searchQuery := query.Select("*, "+scoreQuery,
		keyword,         // 完全匹配群名
		keyword+"%",     // 群名前缀匹配
		"%"+keyword+"%", // 群名包含关键词
	).Where(
		"name LIKE ?", "%"+keyword+"%",
	).Order("relevance_score DESC").
		Limit(20) // 限制返回结果数量

	// 执行查询
	if err := searchQuery.Find(&groups).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "搜索群聊失败",
		})
		return
	}

	// 返回包含groups数组的对象
	c.JSON(http.StatusOK, gin.H{
		"groups": groups,
	})
}

func (s *HTTPServer) Run() error {
	return s.engine.Run(fmt.Sprintf(":%d", config.GlobalConfig.Server.HTTP.Port))
}
