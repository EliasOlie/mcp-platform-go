package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/eliasolie/core-utils/mcp-hub/internal/authclient"
	"github.com/eliasolie/core-utils/mcp-hub/internal/config"
	"github.com/eliasolie/core-utils/mcp-hub/internal/handlers"
	"github.com/eliasolie/core-utils/mcp-hub/internal/registry"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
)

func main() {
	cfg := config.Load()
	log.Printf("[MCP-Hub] Inicializando Gateway de Ferramentas na porta %d...", cfg.Port)

	authCli := authclient.NewClient(cfg.AuthServiceURL, cfg.VPSQLURL)
	reg := registry.NewRegistry(cfg)
	h := handlers.NewHandler(cfg, authCli, reg)

	// Suporte a modo Stdio local
	isStdio := false
	for _, arg := range os.Args[1:] {
		if arg == "--stdio" || arg == "stdio" {
			isStdio = true
			break
		}
	}

	if isStdio {
		log.SetOutput(os.Stderr)
		defaultToken := os.Getenv("MCP_AUTH_TOKEN")
		if defaultToken == "" {
			defaultToken = "eo_master_secret_key_change_me_in_prod"
		}
		if err := h.RunMCPStdio(context.Background(), defaultToken); err != nil {
			log.Fatalf("[FATAL] Erro no loop Stdio do MCP-Hub: %v", err)
		}
		return
	}

	// Servidor HTTP Fiber para Conectores Remotos (Claude, Antigravity, Web)
	app := fiber.New(fiber.Config{
		AppName:               "EliasOlie MCP-Hub Gateway v1.0",
		DisableStartupMessage: false,
	})

	app.Use(recover.New())
	app.Use(logger.New(logger.Config{
		Format: "[${time}] ${status} - ${latency} ${method} ${path}\n",
	}))
	app.Use(cors.New(cors.Config{
		AllowOrigins: "*",
		AllowHeaders: "Origin, Content-Type, Accept, Authorization, X-API-Key",
	}))

	h.RegisterRoutes(app)

	// Graceful Shutdown
	go func() {
		addr := fmt.Sprintf("0.0.0.0:%d", cfg.Port)
		if err := app.Listen(addr); err != nil {
			log.Printf("[HTTP] MCP-Hub finalizado: %v", err)
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	log.Println("[INFO] Encerrando MCP-Hub com segurança...")
	_ = app.Shutdown()
}
