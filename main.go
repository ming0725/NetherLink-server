package main

import (
	"NetherLink-server/config"
	"NetherLink-server/internal/server"
	"NetherLink-server/pkg/database"
	"github.com/gin-gonic/gin"
	"golang.org/x/sync/errgroup"
	"log"
)

func main() {

	if err := config.Init(); err != nil {
		log.Fatal("Failed to load config:", err)
	}

	if _, err := database.GetDB(); err != nil {
		log.Fatal("Failed to connect to database:", err)
	}

	engine := gin.Default()

	httpServer := server.NewHTTPServer(engine)

	wsServer := server.NewWSServer()

	var g errgroup.Group

	g.Go(func() error {
		return httpServer.Run()
	})

	g.Go(func() error {
		return wsServer.Run()
	})

	if err := g.Wait(); err != nil {
		log.Fatal("Server error:", err)
	}
}
