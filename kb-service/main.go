package main

import (
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/gin-gonic/gin"
	"kb-service/internal/handler"
	"kb-service/internal/llm"
	"kb-service/internal/parser"
	"kb-service/internal/storage"
	"kb-service/internal/store"
	"kb-service/internal/worker"
)

func main() {
	// ── 环境变量 ──────────────────────────────────────────────────────────────────
	pgDSN         := getEnv("PG_DSN", "postgres://postgres:postgres@localhost:5432/kbdb?sslmode=disable")
	qdrantURL     := getEnv("QDRANT_URL", "http://localhost:6333")
	embedURL      := getEnv("EMBED_SERVICE_URL", "")
	pythonParser  := getEnv("PYTHON_PARSER_URL", "")
	port          := getEnv("PORT", "9200")

	// ── 本地 OSS（文件存储）─────────────────────────────────────────────────────
	ossBasePath := getEnv("OSS_BASE_PATH", "./data/storage")
	ossBaseURL  := getEnv("OSS_BASE_URL", "http://localhost:"+getEnv("PORT", "9200")+"/storage")
	oss, err := storage.NewLocalStorage(ossBasePath, ossBaseURL)
	if err != nil {
		log.Fatalf("[KB] init local storage failed: %v", err)
	}
	log.Printf("[KB] local OSS ready: %s", ossBasePath)

	// ── PostgreSQL 元数据层 ────────────────────────────────────────────────────────
	pg, err := store.NewPostgresStore(pgDSN)
	if err != nil {
		log.Fatalf("[KB] connect postgres failed: %v", err)
	}
	defer pg.Close()
	log.Printf("[KB] postgres connected: %s", pgDSN)

	// ── Qdrant 向量层 ─────────────────────────────────────────────────────────────
	vec, err := store.NewQdrantStore(qdrantURL)
	if err != nil {
		log.Fatalf("[KB] connect qdrant failed: %v", err)
	}
	log.Printf("[KB] qdrant connected: %s", qdrantURL)

	// ── Embedding 服务 ────────────────────────────────────────────────────────────
	var embedder parser.Embedder
	if embedURL != "" {
		embedder = parser.NewHTTPEmbedder(embedURL)
		log.Printf("[KB] using HTTP embedder: %s", embedURL)
	} else {
		embedder = &parser.MockEmbedder{}
		log.Println("[KB] using MockEmbedder (dev mode)")
	}

	// ── LLM Agent（query 精化，可选）──────────────────────────────────────────────
	var refiner *llm.QueryRefiner
	if getEnv("LLM_API_KEY", "") != "" {
		agent := llm.NewAgent()
		refiner = llm.NewQueryRefiner(agent)
		log.Println("[KB] LLM query refiner enabled")
	} else {
		log.Println("[KB] LLM_API_KEY not set, query refiner disabled")
	}

	// ── Worker ────────────────────────────────────────────────────────────────────
	p := parser.NewSimpleParser(pythonParser)
	w := worker.NewIndexWorker(pg, vec, p, embedder, 256, 4)

	// ── 路由 ──────────────────────────────────────────────────────────────────────
	r := gin.Default()

	collH   := handler.NewCollectionHandler(pg)
	docH    := handler.NewDocumentHandler(pg, vec, w, oss)
	queryH  := handler.NewQueryHandler(vec, embedder, refiner)
	ingestH := handler.NewIngestHandler(pg, embedder, p, w)
	parseH  := handler.NewParseHandler(p)

	api := r.Group("/api/v1/kb")
	{
		api.POST("/collections",                         collH.CreateCollection)
		api.GET("/collections",                          collH.ListCollections)
		api.GET("/collections/:collection_id/documents", collH.ListCollectionDocuments)
		api.POST("/documents",                           docH.IndexDocument)
		api.POST("/upload",                              docH.UploadDocument)
		api.GET("/documents/:doc_id",                    docH.GetDocument)
		api.DELETE("/documents/:doc_id",                 docH.DeleteDocument)
		api.POST("/query",                               queryH.Query)
		api.POST("/ingest-from-search",                  ingestH.IngestFromSearch)
		api.POST("/parse",                               parseH.Parse)
	}

	// ── 静态文件服务（本地 OSS 文件可通过 HTTP 访问）──────────────────────────────
	r.StaticFS("/storage", http.Dir(ossBasePath))

	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"code": 200, "message": "ok"})
	})

	log.Printf("[KB] listening on :%s", port)
	if err := r.Run(":" + port); err != nil {
		log.Fatalf("[KB] server error: %v", err)
	}
}

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func getEnvInt(key string, defaultVal int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return defaultVal
}
