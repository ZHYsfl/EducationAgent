package main

import (
	"log"
	"os"
	"strconv"

	"github.com/gin-gonic/gin"
	"kb-service/internal/handler"
	"kb-service/internal/llm"
	"kb-service/internal/parser"
	"kb-service/internal/store"
	"kb-service/internal/worker"
)

func main() {
	redisAddr     := getEnv("REDIS_ADDR", "localhost:6379")
	redisPassword := getEnv("REDIS_PASSWORD", "")
	redisDB       := getEnvInt("REDIS_DB", 0)
	embedURL      := getEnv("EMBED_SERVICE_URL", "")
	pythonParser  := getEnv("PYTHON_PARSER_URL", "")
	port          := getEnv("PORT", "9200")

	// ── Redis（元数据 + 向量，同一实例）────────────────────────────────────────────
	rdb, err := store.NewRedisStore(redisAddr, redisPassword, redisDB)
	if err != nil {
		log.Fatalf("[KB] connect redis failed: %v", err)
	}
	log.Printf("[KB] redis connected: %s db=%d", redisAddr, redisDB)

	// ── Redis Vector（RediSearch HNSW）─────────────────────────────────────────
	vec, err := store.NewVectorStore(rdb)
	if err != nil {
		log.Fatalf("[KB] init vector store failed: %v", err)
	}
	log.Println("[KB] redis vector index ready")

	// ── Embedding 服务 ────────────────────────────────────────────────────────────
	var embedder parser.Embedder
	if embedURL != "" {
		embedder = parser.NewHTTPEmbedder(embedURL)
		log.Printf("[KB] using HTTP embedder: %s", embedURL)
	} else {
		embedder = &parser.MockEmbedder{}
		log.Println("[KB] using MockEmbedder (dev mode)")
	}

	// ── LLM Agent（tool_calling SDK，用于 query 精化 + 冲突检测）─────────────────
	// LLM_API_KEY 为空时跳过，query 降级为原始文本，不影响主流程
	var refiner *llm.QueryRefiner
	if getEnv("LLM_API_KEY", "") != "" {
		agent := llm.NewAgent()
		refiner = llm.NewQueryRefiner(agent)
		log.Println("[KB] LLM query refiner enabled")
	} else {
		log.Println("[KB] LLM_API_KEY not set, query refiner disabled (fallback to raw query)")
	}

	p := parser.NewSimpleParser(pythonParser)
	w := worker.NewIndexWorker(rdb, vec, p, embedder, 256, 4)

	// ── 路由 ──────────────────────────────────────────────────────────────────────
	r := gin.Default()

	collH   := handler.NewCollectionHandler(rdb)
	docH    := handler.NewDocumentHandler(rdb, vec, w)
	queryH  := handler.NewQueryHandler(vec, embedder, refiner)
	ingestH := handler.NewIngestHandler(rdb, embedder, p, w)
	parseH  := handler.NewParseHandler(p)

	api := r.Group("/api/v1/kb")
	{
		api.POST("/collections",                         collH.CreateCollection)
		api.GET("/collections",                          collH.ListCollections)
		api.GET("/collections/:collection_id/documents", collH.ListCollectionDocuments)
		api.POST("/documents",                           docH.IndexDocument)
		api.GET("/documents/:doc_id",                    docH.GetDocument)
		api.DELETE("/documents/:doc_id",                 docH.DeleteDocument)
		api.POST("/query",                               queryH.Query)
		api.POST("/ingest-from-search",                  ingestH.IngestFromSearch)
		api.POST("/parse",                               parseH.Parse)
	}

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
