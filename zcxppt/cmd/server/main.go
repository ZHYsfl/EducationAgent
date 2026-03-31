package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	sharedoss "educationagent/oss"

	"github.com/redis/go-redis/v9"

	"zcxppt/internal/config"
	"zcxppt/internal/http"
	"zcxppt/internal/http/handlers"
	"zcxppt/internal/http/middleware"
	"zcxppt/internal/infra/llm"
	"zcxppt/internal/infra/oss"
	"zcxppt/internal/infra/renderer"
	"zcxppt/internal/repository"
	"zcxppt/internal/service"
)

func main() {
	cfg := config.Load()
	if err := validateConfig(cfg); err != nil {
		log.Fatalf("invalid configuration: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	redisClient := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})
	if err := redisClient.Ping(ctx).Err(); err != nil {
		log.Fatalf("failed to ping redis: %v", err)
	}

	var taskRepo repository.TaskRepository
	if strings.EqualFold(cfg.TaskRepoMode, "redis") {
		taskRepo = repository.NewRedisTaskRepository(redisClient, time.Duration(cfg.TaskTTLHours)*time.Hour)
	} else {
		taskRepo = repository.NewInMemoryTaskRepository()
	}

	var pptRepo repository.PPTRepository
	if strings.EqualFold(cfg.PPTRepoMode, "redis") {
		pptRepo = repository.NewRedisPPTRepository(redisClient)
	} else {
		pptRepo = repository.NewInMemoryPPTRepository()
	}

	var feedbackRepo repository.FeedbackRepository
	if strings.EqualFold(cfg.FeedbackRepoMode, "redis") {
		feedbackRepo = repository.NewRedisFeedbackRepository(redisClient)
	} else {
		feedbackRepo = repository.NewInMemoryFeedbackRepository()
	}

	var exportRepo repository.ExportRepository
	if strings.EqualFold(cfg.ExportRepoMode, "redis") {
		exportRepo = repository.NewRedisExportRepository(redisClient)
	} else {
		exportRepo = repository.NewInMemoryExportRepository()
	}

	ossClient, err := oss.NewClient(sharedoss.Config{
		Provider:   cfg.OSSProvider,
		Bucket:     cfg.OSSBucket,
		Region:     cfg.OSSRegion,
		SecretID:   cfg.OSSSecretID,
		SecretKey:  cfg.OSSSecretKey,
		SigningKey: cfg.OSSSigningKey,
		BaseURL:    cfg.OSSBaseURL,
		LocalPath:  cfg.OSSLocalPath,
	})
	if err != nil {
		log.Fatalf("failed to init oss client: %v", err)
	}

	notifyService := service.NewNotifyService(cfg.VoiceAgentURL)
	llmRuntime := llm.NewToolRuntime(llm.RuntimeConfig{Mode: cfg.LLMRuntimeMode, APIKey: cfg.LLMAPIKey, Model: cfg.LLMModel, BaseURL: cfg.LLMBaseURL})

	var pptRenderer *renderer.Renderer
	if strings.EqualFold(cfg.RendererMode, "real") {
		pptRenderer = renderer.NewRendererWithConfig(renderer.Config{
			PythonPath:      cfg.PythonPath,
			ScriptPath:     cfg.RenderScriptPath,
			RenderDir:       cfg.RenderDir,
			RenderURLPrefix: cfg.RenderURLPrefix,
			TimeoutSeconds:  cfg.RenderTimeoutSec,
		}, ossClient)
	}

	pptService := service.NewPPTService(taskRepo, pptRepo, feedbackRepo)
	pptService.ConfigureInitGenerator(
		cfg.KBServiceURL,
		service.LLMClientConfig{APIKey: cfg.LLMAPIKey, Model: cfg.LLMModel, BaseURL: cfg.LLMBaseURL},
	)
	if pptRenderer != nil {
		pptService.AttachRenderer(pptRenderer)
	}

	feedbackService := service.NewFeedbackService(
		pptRepo,
		feedbackRepo,
		llmRuntime,
		notifyService,
	)
	if pptRenderer != nil {
		feedbackService.AttachRenderer(pptRenderer)
	}
	exportService := service.NewExportService(exportRepo, ossClient)
	if pptRepo != nil {
		exportService.AttachPPTRepository(pptRepo)
	}

	taskService := service.NewTaskService(taskRepo)
	taskHandler := handlers.NewTaskHandler(taskService)
	pptHandler := handlers.NewPPTHandler(pptService)
	feedbackHandler := handlers.NewFeedbackHandler(feedbackService)
	exportHandler := handlers.NewExportHandler(exportService)
	authMW := middleware.NewAuthMiddleware(cfg.JWTSecret, cfg.InternalKey)

	r := http.NewRouter(taskHandler, pptHandler, feedbackHandler, exportHandler, authMW)

	addr := fmt.Sprintf(":%d", cfg.ServerPort)
	log.Printf("zcxppt service listening on %s", addr)
	if err := r.Run(addr); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}

func validateConfig(cfg config.Config) error {
	if strings.TrimSpace(cfg.RedisAddr) == "" {
		return errors.New("REDIS_ADDR is required")
	}
	if cfg.ServerPort <= 0 {
		return errors.New("ZCXPPT_PORT must be a positive integer")
	}
	if strings.TrimSpace(cfg.InternalKey) == "" {
		return errors.New("INTERNAL_KEY is required")
	}
	return nil
}
