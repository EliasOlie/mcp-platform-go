package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/eliasolie/core-utils/auth-service/internal/config"
	"github.com/eliasolie/core-utils/auth-service/internal/crypto"
	"github.com/eliasolie/core-utils/auth-service/internal/database"
	"github.com/eliasolie/core-utils/auth-service/internal/handlers"
	"github.com/eliasolie/core-utils/auth-service/internal/service"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
)

func main() {
	cfg := config.Load()
	log.Printf("[Auth-Service] Inicializando na porta %d...", cfg.Port)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 1. Conexão com o Banco de Dados
	db, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("[FATAL] Não foi possível conectar ao banco de dados: %v", err)
	}
	defer db.Close()

	// 2. Executar Migrations
	migrationCandidates := []string{"migrations/001_init.sql", "auth-service/migrations/001_init.sql"}
	for _, mPath := range migrationCandidates {
		if _, err := os.Stat(mPath); err == nil {
			if err := db.RunMigrations(context.Background(), mPath); err != nil {
				log.Printf("[WARN] Falha ao rodar migrations do Auth (%s): %v", mPath, err)
			}
			break
		}
	}

	// 3. Inicializar Criptografia Ed25519 e Serviços
	km, err := crypto.NewKeyManager()
	if err != nil {
		log.Fatalf("[FATAL] Erro ao inicializar KeyManager Ed25519: %v", err)
	}
	log.Printf("[Auth-Service] Par de chaves Ed25519 ativo (KID: %s)", km.KeyID())

	authSvc := service.NewAuthService(cfg, db, km)
	if err := authSvc.EnsureBootstrapAdmin(context.Background()); err != nil {
		log.Printf("[WARN] Falha no bootstrap do admin: %v", err)
	}
	h := handlers.NewHandler(cfg, authSvc)

	// 4. Servidor HTTP Fiber de Alta Performance
	app := fiber.New(fiber.Config{
		AppName:               "EliasOlie Auth Platform v1.0",
		DisableStartupMessage: false,
	})

	app.Use(recover.New())
	app.Use(logger.New(logger.Config{
		Format: "[${time}] ${status} - ${latency} ${method} ${path}\n",
	}))
	app.Use(cors.New(cors.Config{
		AllowOrigins: "*",
		AllowHeaders: "Origin, Content-Type, Accept, Authorization, X-Master-Key",
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

	log.Println("[INFO] Encerrando Auth Service com segurança...")
	_ = app.Shutdown()
}
