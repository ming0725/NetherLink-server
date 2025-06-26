package main

import (
	"NetherLink-server/config"
	"NetherLink-server/internal/server"
	"NetherLink-server/pkg/database"
	"github.com/gin-gonic/gin"
	"golang.org/x/sync/errgroup"
	"log"
)

//TIP <p>To run your code, right-click the code and select <b>Run</b>.</p> <p>Alternatively, click
// the <icon src="AllIcons.Actions.Execute"/> icon in the gutter and select the <b>Run</b> menu item from here.</p>

func main() {
  //TIP <p>Press <shortcut actionId="ShowIntentionActions"/> when your caret is at the underlined text
  // to see how GoLand suggests fixing the warning.</p><p>Alternatively, if available, click the lightbulb to view possible fixes.</p>

	// 初始化配置
	if err := config.Init(); err != nil {
		log.Fatal("Failed to load config:", err)
	}

	// 初始化数据库连接
	if _, err := database.GetDB(); err != nil {
		log.Fatal("Failed to connect to database:", err)
	}

	// 创建Gin引擎
	engine := gin.Default()

	// 创建HTTP服务器（包含AI WebSocket服务）
	httpServer := server.NewHTTPServer(engine)
	
	// 创建普通聊天的WebSocket服务器
	wsServer := server.NewWSServer()

	// 使用errgroup同时运行两个服务器
	var g errgroup.Group
	
	// 运行HTTP服务器（端口8080）
	g.Go(func() error {
		return httpServer.Run()
	})
	
	// 运行普通聊天的WebSocket服务器（端口8081）
	g.Go(func() error {
		return wsServer.Run()
	})

	// 等待服务器退出
	if err := g.Wait(); err != nil {
		log.Fatal("Server error:", err)
	}
}