# NetherLink-server 🚀

NetherLink-server 是一个基于 Go 语言开发的即时通讯服务器，提供用户管理、即时通讯、社交动态等功能。

## ✨ 主要功能

1. 👤 用户系统
   - 📧 邮箱验证码注册
   - 🔐 账号密码登录
   - 🎫 JWT 身份认证

2. 💬 即时通讯
   - 🔌 WebSocket 实时通讯
   - 📨 私聊消息
   - 👥 群聊功能
   - 👬 好友系统
   - 🟢 多种在线状态显示

3. 📱 社交动态
   - 📝 发布动态（支持文字和图片）
   - 💭 评论功能
   - ❤️ 点赞功能

4. 🤖 AI 对话
   - 🧠 基于 Deepseek API 的智能对话
   - 📚 对话历史记录
   - ⚡ 流式响应

## 🛠️ 技术栈

- 🐹 Go
- 🍸 Gin (Web 框架)
- 🗃️ GORM (ORM 框架)
- 🔌 WebSocket
- 🐬 MySQL
- 🎫 JWT

## 🚀 快速开始

### 1. 配置数据库 📊

1. 创建 MySQL 数据库
```mysql
CREATE DATABASE netherlink;
```

2. 导入数据库结构
```bash
mysql -u your_username -p netherlink < netherlink.sql
```

### 2. 修改配置文件 ⚙️

配置文件位于 `config/config.yaml`，需要修改以下配置：

1. 服务器配置
```yaml
server:
  http:
    base_url: http://your-domain:8080  # 修改为你的服务器地址
```

2. 数据库配置
```yaml
database:
  host: localhost     # 数据库地址
  port: 3306         # 数据库端口
  username: root     # 数据库用户名
  password: 123456   # 数据库密码
  dbname: netherlink # 数据库名称
```

3. JWT 配置
```yaml
jwt:
  secret: your-jwt-secret  # 修改为自定义的 JWT 密钥
```

4. AI 配置（如果需要 AI 对话功能）
```yaml
ai:
  api_key: your-deepseek-api-key  # 替换为你的 Deepseek API 密钥
```

5. 邮箱配置（用于发送验证码）
```yaml
email:
  smtp_host: smtp.qq.com          # SMTP 服务器地址
  smtp_port: 465                  # SMTP 端口
  sender: "your-email@qq.com"     # 发件人邮箱
  password: "your-email-password" # 邮箱授权码
```

### 3. 运行服务器 🚀

```bash
go run main.go
```

### 4. 客户端仓库地址 🌐

- 客户端代码仓库：[NetherLink](https://github.com/ming0725/NetherLink)

- 按照客户端仓库的说明进行部署

## 📚 API 文档

### 👤 用户相关

1. 发送验证码
- POST `/api/send_code`
- 用于注册时发送邮箱验证码

2. 用户注册
- POST `/api/register`
- 需要验证码验证

3. 用户登录
- POST `/api/login`
- 返回 JWT token

### 🔌 WebSocket 连接

1. 聊天服务
- WebSocket `/ws`
- 需要 JWT 认证

2. AI 对话服务
- WebSocket `/ws/ai`
- 需要 JWT 认证

### 🤝 社交功能

1. 好友相关
- GET `/api/contacts` - 获取联系人列表
- GET `/api/search/users` - 搜索用户
- GET `/api/search/groups` - 搜索群组

2. 动态相关
- GET `/api/posts` - 获取动态列表
- POST `/api/posts` - 发布动态
- GET `/api/posts/:post_id` - 获取动态详情
- POST `/api/posts/:post_id/comments` - 发表评论
- POST `/api/posts/:post_id/like` - 点赞/取消点赞

## 📁 目录结构

```
NetherLink-server/
├── config/           # 配置文件
├── internal/         # 内部包
│   ├── model/       # 数据模型
│   └── server/      # 服务器实现
├── pkg/             # 公共包
│   ├── database/    # 数据库相关
│   └── utils/       # 工具函数
├── uploads/         # 上传文件目录
└── main.go          # 程序入口
```

## ⚠️ 注意事项

1. 🔒 首次部署时请修改配置文件中的敏感信息
2. 🛡️ 建议在生产环境中使用 HTTPS
3. 📝 确保上传目录具有适当的写入权限
4. 💾 建议定期备份数据库

## 📄 License

[MIT License](LICENSE) 