package handler

import (
	"context"
	"log"
	"math"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"kb-service/internal/llm"
	"kb-service/internal/metrics"
	"kb-service/internal/model"
	"kb-service/internal/parser"
	"kb-service/internal/store"
	"kb-service/pkg/util"
)

// ── BM25 查询评分器 ────────────────────────────────────────────────────────

type queryBM25 struct {
	avgDL  float64
	idfMap map[string]float64
	k1     float64
	b      float64
}

func newQueryBM25(tokens []string) *queryBM25 {
	bm := &queryBM25{k1: 1.5, b: 0.75}
	freq := make(map[string]int)
	for _, t := range tokens {
		freq[t]++
	}
	dl := float64(len(tokens))
	bm.avgDL = dl
	N := float64(10000) // 估算总文档数，保证 idf 为正
	bm.idfMap = make(map[string]float64)
	for term, tf := range freq {
		idf := math.Log((N - float64(tf) + 0.5) / (float64(tf) + 0.5))
		if idf < 0.1 {
			idf = 0.1
		}
		bm.idfMap[term] = idf
	}
	return bm
}

func (bm *queryBM25) Score(tokens []string) ([]uint32, []float32) {
	freq := make(map[string]int)
	for _, t := range tokens {
		freq[t]++
	}
	type kv struct{ term string; score float64 }
	var kvs []kv
	for term, tf := range freq {
		idf := bm.idfMap[term]
		norm := float64(tf) * (bm.k1 + 1) / (float64(tf) + bm.k1*(1-bm.b+bm.b*float64(len(tokens))/bm.avgDL))
		kvs = append(kvs, kv{term, idf * norm})
	}
	sort.Slice(kvs, func(i, j int) bool { return kvs[i].score > kvs[j].score })
	if len(kvs) > 32 {
		kvs = kvs[:32]
	}
	indices := make([]uint32, len(kvs))
	values := make([]float32, len(kvs))
	for i, kv := range kvs {
		indices[i] = uint32(i)
		values[i] = float32(kv.score)
	}
	return indices, values
}

func queryTokenize(text string) []string {
	var tokens []string
	seen := make(map[string]bool)
	stopWords := map[string]bool{
		"的": true, "了": true, "是": true, "在": true, "和": true, "与": true,
		"或": true, "而": true, "及": true, "等": true, "对": true,
		"于": true, "为": true, "以": true, "有": true, "这": true, "那": true,
		"the": true, "a": true, "an": true, "of": true, "in": true, "to": true,
		"and": true, "is": true, "for": true, "on": true, "with": true, "as": true,
		"by": true, "at": true, "from": true, "that": true, "this": true,
	}
	// 简单中文字符分词 + 英文 token
	for _, r := range text {
		if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) ||
			unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r) {
			t := strings.ToLower(string(r))
			if !seen[t] && !stopWords[t] {
				tokens = append(tokens, t)
				seen[t] = true
			}
		}
	}
	// 英文/数字词
	for _, word := range strings.FieldsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		w := strings.ToLower(strings.Trim(word, " \t"))
		if len(w) >= 2 && !seen[w] && !stopWords[w] {
			tokens = append(tokens, w)
			seen[w] = true
		}
	}
	return tokens
}

// QueryHandler 处理语义检索接口
type QueryHandler struct {
	vec      store.VecStore
	embedder parser.Embedder
	refiner  *llm.QueryRefiner // 模糊自然语言 query 意图精化，nil = 不启用
	reranker Reranker          // Rerank 模型，nil = 不启用
}

// NewQueryHandler 创建 QueryHandler
// reranker 可为 nil，表示不启用 rerank
func NewQueryHandler(vec store.VecStore, embedder parser.Embedder, refiner *llm.QueryRefiner, reranker Reranker) *QueryHandler {
	return &QueryHandler{vec: vec, embedder: embedder, refiner: refiner, reranker: reranker}
}

// Query POST /api/v1/kb/query
// 核心 RAG 语义检索接口（规范 §4.3.5）
// 流程：query → [LLM意图精化] → 向量化 + BM25 → [Hybrid/Rerank] → 结果
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

	hybrid := "false"
	if req.Hybrid {
		hybrid = "true"
	}
	rerank := "false"
	if req.Rerank {
		rerank = "true"
	}

	// ── 自然语言意图精化 ───────────────────────────────────────────────────
	effectiveQuery := req.Query
	if h.refiner != nil {
		effectiveQuery = h.refiner.Refine(c.Request.Context(), req.Query)
	}

	// ── 向量化（dense）──────────────────────────────────────────────────
	vecs, err := h.embedder.Embed(c.Request.Context(), []string{effectiveQuery})
	if err != nil {
		metrics.QueryTotal.WithLabelValues(hybrid, rerank, "embed_error").Inc()
		util.Fail(c, util.CodeDependencyUnavailable, "向量化失败: "+err.Error())
		return
	}
	if len(vecs) == 0 {
		metrics.QueryTotal.WithLabelValues(hybrid, rerank, "embed_empty").Inc()
		util.Fail(c, util.CodeDependencyUnavailable, "向量化返回为空")
		return
	}

	// ── 混合检索：Dense 检索 → 本地 BM25 关键词评分 ────────────────────
	queryTokens := queryTokenize(effectiveQuery)
	var sparseIndices []uint32
	var sparseValues []float32
	if len(queryTokens) > 0 {
		bm := newQueryBM25(queryTokens)
		sparseIndices, sparseValues = bm.Score(queryTokens)
	}

	// ── 解析过滤器 ───────────────────────────────────────────────────────
	searchReq := store.SearchChunksReq{
		Vector:          vecs[0],
		SparseVector:    sparseIndices,
		SparseValues:    sparseValues,
		QueryText:      effectiveQuery,
		UserID:          req.UserID,
		CollectionID:    req.CollectionID,
		TopK:            req.TopK,
		ScoreThreshold:  req.ScoreThreshold,
		Hybrid:          req.Hybrid,
		Rerank:          req.Rerank,
		DenseWeight:     req.DenseWeight,
		RerankTopK:      req.RerankTopK,
	}
	// 从请求的 filters 解析（兼容旧的 map 格式）
	if req.Filters != nil {
		if v, ok := req.Filters["source_type"]; ok {
			searchReq.Filters.SourceType = v
		}
		if v, ok := req.Filters["origin"]; ok {
			searchReq.Filters.Origin = v
		}
		if v, ok := req.Filters["date_from"]; ok {
			if f, ok := parseInt64(v); ok {
				searchReq.Filters.DateFrom = f
			}
		}
		if v, ok := req.Filters["date_to"]; ok {
			if f, ok := parseInt64(v); ok {
				searchReq.Filters.DateTo = f
			}
		}
	}

	// ── 语义检索 ─────────────────────────────────────────────────────────
	chunks, err := h.vec.SearchChunks(c.Request.Context(), searchReq)
	if err != nil {
		metrics.QueryTotal.WithLabelValues(hybrid, rerank, "search_error").Inc()
		util.Fail(c, util.CodeDependencyUnavailable, "向量检索失败: "+err.Error())
		return
	}
	if chunks == nil {
		chunks = []model.RetrievedChunk{}
	}

	// ── Rerank（语义重排序）──────────────────────────────────────────────
	if h.reranker != nil && len(chunks) > 0 && req.Rerank {
		rerankTopK := req.RerankTopK
		if rerankTopK <= 0 {
			rerankTopK = 20
		}
		fetchTopK := rerankTopK
		if fetchTopK > len(chunks) {
			fetchTopK = len(chunks)
		}
		reranked, err := h.reranker.Rerank(c.Request.Context(), effectiveQuery, chunks[:fetchTopK])
		if err != nil {
			log.Printf("[QueryHandler] rerank failed, using original results: %v", err)
		} else {
			chunks = reranked
		}
	}

	// ── 指标埋点 ────────────────────────────────────────────────────────
	n := len(chunks)
	metrics.QueryChunksReturned.WithLabelValues(hybrid).Observe(float64(n))
	metrics.QueryTotal.WithLabelValues(hybrid, rerank, "success").Inc()
	log.Printf("[Query] user=%s query=%q hybrid=%v rerank=%v filters=%v results=%d",
		req.UserID, effectiveQuery, req.Hybrid, req.Rerank, req.Filters, n)

	util.OK(c, model.KBQueryResponse{
		Chunks: chunks,
		Total:  n,
	})
}

// parseInt64 安全解析字符串为 int64
func parseInt64(s string) (int64, bool) {
	var v int64
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
		v = v*10 + int64(c-'0')
	}
	return v, true
}
