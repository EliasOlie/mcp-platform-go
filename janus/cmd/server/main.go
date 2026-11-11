package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/eliasolie/janus/internal/config"
	"github.com/eliasolie/janus/internal/database"
	"github.com/eliasolie/janus/internal/embedding"
	"github.com/eliasolie/janus/internal/handlers"
	"github.com/eliasolie/janus/internal/search"
	"github.com/eliasolie/janus/internal/service"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
)

func main() {
	cfg := config.Load()

	log.Printf("Iniciando Janus Core (Cérebro Operacional) na porta %d...", cfg.Port)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 1. Conexão com o Banco de Dados
	db, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("[FATAL] Não foi possível conectar ao banco de dados: %v", err)
	}
	defer db.Close()

	// 2. Executar Migrations (se o arquivo existir)
	migrationPath := "migrations/001_init.sql"
	if _, err := os.Stat(migrationPath); err == nil {
		if err := db.RunMigrations(context.Background(), migrationPath); err != nil {
			log.Printf("[WARN] Falha ao rodar migration inicial: %v (se pgvector ainda não foi compilado no container, ignore temporariamente)", err)
		}
	}

	// 3. Inicializar Providers e Camadas de Negócio
	embedder := embedding.NewProvider(cfg)
	searchEngine := search.NewEngine(db, embedder, cfg.SearchTopK)
	svc := service.NewService(db, embedder)
	h := handlers.New(cfg, db, svc, searchEngine)

	// Se iniciado com flag --stdio, opera em modo MCP Standard I/O para agy, Claude Code e Cursor
	isStdio := false
	for _, arg := range os.Args[1:] {
		if arg == "--stdio" || arg == "stdio" {
			isStdio = true
			break
		}
	}

	if isStdio {
		log.SetOutput(os.Stderr)
		if err := h.RunMCPStdio(context.Background()); err != nil {
			log.Fatalf("[FATAL] Erro no loop MCP Stdio: %v", err)
		}
		return
	}

	// 4. Servidor HTTP Fiber
	app := fiber.New(fiber.Config{
		AppName:               "Janus Core (Brain) v1.0",
		DisableStartupMessage: false,
	})

	app.Use(recover.New())
	app.Use(logger.New(logger.Config{
		Format: "[${time}] ${status} - ${latency} ${method} ${path}\n",
	}))
	app.Use(cors.New(cors.Config{
		AllowOrigins: "*",
		AllowHeaders: "Origin, Content-Type, Accept, Authorization",
	}))

	h.RegisterRoutes(app)

	// Graceful Shutdown
	go func() {
		addr := fmt.Sprintf("0.0.0.0:%d", cfg.Port)
		if err := app.Listen(addr); err != nil {
			log.Printf("[HTTP] Servidor finalizado: %v", err)
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	log.Println("[INFO] Encerrando Janus Core com segurança...")
	_ = app.Shutdown()
}
