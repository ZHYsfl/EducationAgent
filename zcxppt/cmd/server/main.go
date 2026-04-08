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
	"zcxppt/internal/infra"
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

	// RefFusionService: per-file parsing + targeted extraction + style mapping
	refParser := infra.NewFileParser(cfg.OCRBaseURL, cfg.TransBaseURL)
	refFusion := service.NewRefFusionService(refParser, service.LLMClientConfig{
		APIKey:  cfg.LLMAPIKey,
		Model:   cfg.LLMModel,
		BaseURL: cfg.LLMBaseURL,
	}, cfg.KBServiceURL)

	var pptRenderer *renderer.Renderer
	if strings.EqualFold(cfg.RendererMode, "real") {
		pptRenderer = renderer.NewRendererWithConfig(renderer.Config{
			PythonPath:      cfg.PythonPath,
			ScriptPath:      cfg.RenderScriptPath,
			RenderDir:       cfg.RenderDir,
			RenderURLPrefix: cfg.RenderURLPrefix,
			TimeoutSeconds:  cfg.RenderTimeoutSec,
		}, ossClient)
	}

	pptService := service.NewPPTService(taskRepo, pptRepo, feedbackRepo)
	pptService.ConfigureInitGenerator(
		cfg.KBServiceURL,
		service.LLMClientConfig{
			APIKey:    cfg.LLMAPIKey,
			Model:     cfg.LLMModel,
			BaseURL:   cfg.LLMBaseURL,
			KBToolURL: cfg.KBToolURL,
		},
	)
	if pptRenderer != nil {
		pptService.AttachRenderer(pptRenderer)
	}
	pptService.AttachRefFusionService(refFusion)

	feedbackService := service.NewFeedbackService(
		pptRepo,
		feedbackRepo,
		llmRuntime,
		notifyService,
	)
	feedbackService.AttachLLMConfig(service.LLMClientConfig{
		APIKey:  cfg.LLMAPIKey,
		Model:   cfg.LLMModel,
		BaseURL: cfg.LLMBaseURL,
	})
	if pptRenderer != nil {
		feedbackService.AttachRenderer(pptRenderer)
	}
	feedbackService.AttachRefFusionService(refFusion)
	// 注入三路合并服务
	mergeService := service.NewMergeService()
	feedbackService.AttachMergeService(mergeService)

	// IntentParser: parses RawText -> []Intent when Voice Agent sends empty Intents
	intentParser := service.NewIntentParser(pptRepo, service.LLMClientConfig{
		APIKey:  cfg.LLMAPIKey,
		Model:   cfg.LLMModel,
		BaseURL: cfg.LLMBaseURL,
	})

	exportService := service.NewExportService(exportRepo, ossClient)
	if pptRepo != nil {
		exportService.AttachPPTRepository(pptRepo)
	}

	teachingPlanService := service.NewTeachingPlanService(
		service.LLMClientConfig{
			APIKey:  cfg.LLMAPIKey,
			Model:   cfg.LLMModel,
			BaseURL: cfg.LLMBaseURL,
		},
		service.RenderServiceConfig{
			PythonPath: cfg.PythonPath,
			ScriptPath: cfg.RenderScriptPath,
			RenderDir:  cfg.RenderDir,
			URLPrefix:  cfg.RenderURLPrefix,
		},
		ossClient,
	)

	contentDiversityService := service.NewContentDiversityService(
		service.LLMClientConfig{
			APIKey:  cfg.LLMAPIKey,
			Model:   cfg.LLMModel,
			BaseURL: cfg.LLMBaseURL,
		},
		service.RenderServiceConfig{
			PythonPath: cfg.PythonPath,
			ScriptPath: cfg.RenderScriptPath,
			RenderDir:  cfg.RenderDir,
			URLPrefix:  cfg.RenderURLPrefix,
		},
		ossClient,
	)

	// 联动：Init 时自动生成教案和内容多样性（解耦并发）
	pptService.AttachTeachingPlanService(teachingPlanService)
	pptService.AttachContentDiversityService(contentDiversityService)
	pptService.AttachNotifier(notifyService)
	pptService.AttachFeedbackService(feedbackService)

	// 注入 contentDiversityService 到 feedbackService（用于处理 generate_animation/generate_game intent）
	feedbackService.AttachContentDiversityService(contentDiversityService)
	// 注入 NotifyService 到 ContentDiversityService（生成完成后回调 Voice Agent）
	contentDiversityService.AttachNotifier(notifyService)

	pptHandler := handlers.NewPPTHandler(pptService)
	feedbackHandler := handlers.NewFeedbackHandler(feedbackService, intentParser)
	exportHandler := handlers.NewExportHandler(exportService)
	teachingPlanHandler := handlers.NewTeachingPlanHandler(teachingPlanService)
	contentDiversityHandler := handlers.NewContentDiversityHandler(contentDiversityService)
	authMW := middleware.NewAuthMiddleware(cfg.JWTSecret, cfg.InternalKey)

	r := http.NewRouter(pptHandler, feedbackHandler, exportHandler, teachingPlanHandler, contentDiversityHandler, authMW)

	startTimeoutTicker(feedbackService)

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

func startTimeoutTicker(feedbackService *service.FeedbackService) {
	go func() {
		ticker := time.NewTicker(45 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			ctx := context.Background()
			if err := feedbackService.ProcessTimeoutTick(ctx); err != nil {
				log.Printf("[timeout_ticker] tick failed: %v", err)
			}
		}
	}()
}
