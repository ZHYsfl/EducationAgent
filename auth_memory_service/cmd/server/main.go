package main

import (
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"auth_memory_service/internal/config"
	transport "auth_memory_service/internal/http"
	"auth_memory_service/internal/http/handlers"
	"auth_memory_service/internal/http/middleware"
	"auth_memory_service/internal/infra/extractor"
	jwtinfra "auth_memory_service/internal/infra/jwt"
	"auth_memory_service/internal/infra/mailer"
	"auth_memory_service/internal/repository"
	"auth_memory_service/internal/service"
)

func main() {
	cfg := config.Load()
	if cfg.PostgresDSN == "" {
		log.Fatal("POSTGRES_DSN is required")
	}
	db, err := gorm.Open(postgres.Open(cfg.PostgresDSN), &gorm.Config{})
	if err != nil {
		log.Fatalf("failed to connect database: %v", err)
	}
	repo := repository.NewAuthRepository(db)
	memRepo := repository.NewMemoryRepository(db)
	redisClient := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})
	workingRepo := repository.NewWorkingMemoryRepository(redisClient, time.Duration(cfg.WorkingMemoryTTLHrs)*time.Hour)
	tm := jwtinfra.NewTokenManager(cfg.JWTSecret, cfg.JWTTTLHours)
	authService := service.NewAuthService(repo, tm, mailer.NoopMailer{}, cfg.VerifyTokenTTLHours, cfg.FrontendVerifyURL)
	memoryService := service.NewMemoryService(repo, memRepo, workingRepo, extractor.RuleBasedExtractor{})
	authHandler := handlers.NewAuthHandler(authService)
	memoryHandler := handlers.NewMemoryHandler(memoryService)
	authMW := middleware.NewAuthMiddleware(tm, cfg.InternalKey)
	r := transport.NewRouter(authHandler, authMW)
	transport.AddMemoryRoutes(r, memoryHandler, authMW)
	addr := fmt.Sprintf(":%d", cfg.ServerPort)
	if err := r.Run(addr); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}
