package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
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
	if err := validateConfig(cfg); err != nil {
		log.Fatalf("invalid configuration: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	db, err := gorm.Open(postgres.Open(cfg.PostgresDSN), &gorm.Config{})
	if err != nil {
		log.Fatalf("failed to connect database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		log.Fatalf("failed to initialize database pool: %v", err)
	}
	if err := sqlDB.PingContext(ctx); err != nil {
		log.Fatalf("failed to ping database: %v", err)
	}

	repo := repository.NewAuthRepository(db)
	memRepo := repository.NewMemoryRepository(db)
	redisClient := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})
	if err := redisClient.Ping(ctx).Err(); err != nil {
		log.Fatalf("failed to ping redis: %v", err)
	}
	workingRepo := repository.NewWorkingMemoryRepository(redisClient, time.Duration(cfg.WorkingMemoryTTLHrs)*time.Hour)
	tm := jwtinfra.NewTokenManager(cfg.JWTSecret, cfg.JWTTTLHours)
	authService := service.NewAuthService(repo, tm, mailer.NoopMailer{}, cfg.VerifyTokenTTLHours, cfg.FrontendVerifyURL)
	ex := newMemoryExtractor(cfg)
	memoryService := service.NewMemoryService(repo, memRepo, workingRepo, ex)
	authHandler := handlers.NewAuthHandler(authService)
	memoryHandler := handlers.NewMemoryHandler(memoryService)
	authMW := middleware.NewAuthMiddleware(tm, cfg.InternalKey)
	r := transport.NewRouter(authHandler, authMW)
	transport.AddMemoryRoutes(r, memoryHandler, authMW)
	addr := fmt.Sprintf(":%d", cfg.ServerPort)
	log.Printf("auth-memory service listening on %s", addr)
	if err := r.Run(addr); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}

func newMemoryExtractor(cfg config.Config) *extractor.HybridExtractor {
	var llmClient extractor.LLMClient
	if cfg.ExtractorLLMEnabled {
		client, err := extractor.NewDeepSeekClient(
			cfg.DeepSeekAPIKey,
			cfg.DeepSeekBaseURL,
			cfg.ExtractorLLMModel,
			time.Duration(cfg.ExtractorTimeoutMS)*time.Millisecond,
		)
		if err != nil {
			log.Printf("deepseek extractor disabled: %v", err)
		} else {
			llmClient = client
		}
	}
	return extractor.NewHybridExtractor(extractor.Config{
		EnableLLM: cfg.ExtractorLLMEnabled,
		LLMModel:  cfg.ExtractorLLMModel,
		Timeout:   time.Duration(cfg.ExtractorTimeoutMS) * time.Millisecond,
		MaxTurns:  cfg.ExtractorMaxTurns,
	}, llmClient)
}

func validateConfig(cfg config.Config) error {
	if strings.TrimSpace(cfg.PostgresDSN) == "" {
		return errors.New("POSTGRES_DSN is required")
	}
	if strings.TrimSpace(cfg.RedisAddr) == "" {
		return errors.New("REDIS_ADDR is required")
	}
	if strings.TrimSpace(cfg.InternalKey) == "" {
		return errors.New("INTERNAL_KEY is required")
	}
	if strings.TrimSpace(cfg.JWTSecret) == "" || cfg.JWTSecret == "change-me" {
		return errors.New("JWT_SECRET must be set to a non-default value")
	}
	if cfg.ServerPort <= 0 {
		return errors.New("AUTH_MEMORY_PORT must be a positive integer")
	}
	return nil
}
