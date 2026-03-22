package handler

import (
	"github.com/gin-gonic/gin"
	"kb-service/internal/llm"
	"kb-service/internal/model"
	"kb-service/internal/parser"
	"kb-service/internal/store"
	"kb-service/pkg/util"
)

// QueryHandler 处理语义检索接口
type QueryHandler struct {
	vec      store.VecStore
	embedder parser.Embedder
	refiner  *llm.QueryRefiner // 模糊自然语言 query 意图精化，nil = 不启用
}

func NewQueryHandler(vec store.VecStore, embedder parser.Embedder, refiner *llm.QueryRefiner) *QueryHandler {
	return &QueryHandler{vec: vec, embedder: embedder, refiner: refiner}
}

// Query POST /api/v1/kb/query
// 核心 RAG 语义检索接口（规范 §4.3.5）
// 流程：自然语言 query → [LLM意图精化] → 向量化 → Redis Vector KNN 检索
func (h *QueryHandler) Query(c *gin.Context) {
	var req model.KBQueryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		util.Fail(c, util.CodeParamError, "请求体解析失败: "+err.Error())
		return
	}
	if req.UserID == "" {
		util.Fail(c, util.CodeParamError, "user_id 不能为空")
		return
	}
	if req.Query == "" {
		util.Fail(c, util.CodeParamError, "query 不能为空")
		return
	}

	// 默认值
	if req.TopK <= 0 {
		req.TopK = 5
	}
	if req.TopK > 20 {
		req.TopK = 20
	}
	if req.ScoreThreshold <= 0 {
		req.ScoreThreshold = 0.5
	}

	// ── 自然语言意图精化（高度模糊 query 动态处理）───────────────────────────────
	// 不使用正则/硬编码，由 LLM 理解语义后扩写为精确检索词
	// refiner 不可用时自动降级为原始 query，不阻断主流程
	effectiveQuery := req.Query
	if h.refiner != nil {
		effectiveQuery = h.refiner.Refine(c.Request.Context(), req.Query)
	}

	// ── 向量化 ───────────────────────────────────────────────────────────────────
	vecs, err := h.embedder.Embed(c.Request.Context(), []string{effectiveQuery})
	if err != nil {
		util.Fail(c, util.CodeDependencyUnavailable, "向量化失败: "+err.Error())
		return
	}
	if len(vecs) == 0 {
		util.Fail(c, util.CodeDependencyUnavailable, "向量化返回为空")
		return
	}

	// ── 语义检索 ─────────────────────────────────────────────────────────────────
	chunks, err := h.vec.SearchChunks(c.Request.Context(), store.SearchChunksReq{
		Vector:         vecs[0],
		UserID:         req.UserID,
		CollectionID:   req.CollectionID,
		TopK:           req.TopK,
		ScoreThreshold: req.ScoreThreshold,
	})
	if err != nil {
		util.Fail(c, util.CodeDependencyUnavailable, "向量检索失败: "+err.Error())
		return
	}
	if chunks == nil {
		chunks = []model.RetrievedChunk{}
	}
	util.OK(c, model.KBQueryResponse{
		Chunks: chunks,
		Total:  len(chunks),
	})
}
