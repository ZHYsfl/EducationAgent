package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"sort"
	"strings"
	"unicode"

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
	N := float64(10000)
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

// Tokenize 导出版，供 qdrant.go 混合检索使用
func Tokenize(text string) []string {
	return queryTokenize(text)
}

// QueryHandler 处理语义检索接口
type QueryHandler struct {
	vec          store.VecStore
	embedder     parser.Embedder
	refiner      *llm.QueryRefiner
	reranker     Reranker
	voiceCBURL   string // voice-agent 回调 URL，异步模式使用
	httpClient   *http.Client
}

var _ QueryServicer = (*QueryHandler)(nil)

// NewQueryHandler 创建 QueryHandler
func NewQueryHandler(vec store.VecStore, embedder parser.Embedder, refiner *llm.QueryRefiner, reranker Reranker, voiceCallbackURL string) *QueryHandler {
	return &QueryHandler{
		vec:        vec,
		embedder:   embedder,
		refiner:    refiner,
		reranker:   reranker,
		voiceCBURL: voiceCallbackURL,
		httpClient: &http.Client{Timeout: 10 * 1000000000},
	}
}

// QueryServicer 接口：暴露 DoQuery 给其他包调用
type QueryServicer interface {
	DoQuery(ctx context.Context, req model.KBQueryRequest) ([]model.RetrievedChunk, error)
}

// DoQuery 实现 QueryServicer，供其他包调用（不带 gin.Context 的同步查询）
func (h *QueryHandler) DoQuery(ctx context.Context, req model.KBQueryRequest) ([]model.RetrievedChunk, error) {
	chunks, err := h.vec.SearchChunks(ctx, store.SearchChunksReq{
		UserID:    req.UserID,
		QueryText: req.Query,
		TopK:      req.TopK,
	})
	return chunks, err
}

// Query POST /api/v1/kb/query
// 支持两种模式：
//   - 异步模式（传 session_id）：立即返回 accepted:true，完成后回调 voice-agent
//   - 同步模式（不传 session_id）：立即返回 summary
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

	// BUG 1.2 修复：top_k 必须 > 0，不传或传 0 应返回参数错误
	if req.TopK <= 0 {
		util.Fail(c, util.CodeParamError, "top_k 必须大于 0")
		return
	}
	if req.TopK > 20 {
		req.TopK = 20
	}
	// BUG 1.3 修复：score_threshold=0.0 表示不设阈值，不再静默覆盖为 0.5
	// 允许 0.0（不设阈值），但拒绝 >1.0 的非法值
	if req.ScoreThreshold > 1.0 {
		util.Fail(c, util.CodeParamError, "score_threshold 不能大于 1.0")
		return
	}

	if req.SessionID != "" {
		// ── 异步模式：立即返回 accepted:true，后台执行检索并回调 ───────────
		util.OK(c, gin.H{"accepted": true})

		go func() {
			summary, err := h.doQueryAndSummarize(context.Background(), req)
			h.sendVoiceCallback(req.SessionID, summary, err)
		}()
		return
	}

	// ── 同步模式：立即执行检索 + 生成摘要 ──────────────────────────────────
	summary, err := h.doQueryAndSummarize(c.Request.Context(), req)
	if err != nil {
		util.Fail(c, util.CodeDependencyUnavailable, err.Error())
		return
	}

	util.OK(c, gin.H{"summary": summary})
}

// doQueryAndSummarize 执行一次完整检索（去重后取 topK 作为摘要上下文）
func (h *QueryHandler) doQueryAndSummarize(ctx context.Context, req model.KBQueryRequest) (string, error) {
	hybrid := "false"
	if req.Hybrid {
		hybrid = "true"
	}
	rerank := "false"
	if req.Rerank {
		rerank = "true"
	}

	effectiveQuery := req.Query
	if h.refiner != nil {
		effectiveQuery = h.refiner.Refine(ctx, req.Query)
	}

	vecs, err := h.embedder.Embed(ctx, []string{effectiveQuery})
	if err != nil {
		metrics.QueryTotal.WithLabelValues(hybrid, rerank, "embed_error").Inc()
		return "", fmt.Errorf("向量化失败: %w", err)
	}
	if len(vecs) == 0 {
		metrics.QueryTotal.WithLabelValues(hybrid, rerank, "embed_empty").Inc()
		return "", fmt.Errorf("向量化返回为空")
	}

	queryTokens := queryTokenize(effectiveQuery)
	var sparseIndices []uint32
	var sparseValues []float32
	if len(queryTokens) > 0 {
		bm := newQueryBM25(queryTokens)
		sparseIndices, sparseValues = bm.Score(queryTokens)
	}

	searchReq := store.SearchChunksReq{
		Vector:         vecs[0],
		SparseVector:   sparseIndices,
		SparseValues:   sparseValues,
		QueryText:      effectiveQuery,
		UserID:         req.UserID,
		CollectionID:   req.CollectionID,
		TopK:           req.TopK,
		ScoreThreshold: req.ScoreThreshold,
		Hybrid:         req.Hybrid,
		Rerank:         req.Rerank,
		DenseWeight:    req.DenseWeight,
		RerankTopK:    req.RerankTopK,
	}
	if req.Filters != nil {
		if v, ok := req.Filters["source_type"]; ok {
			searchReq.Filters.SourceType = fmt.Sprintf("%v", v)
		}
		if v, ok := req.Filters["origin"]; ok {
			searchReq.Filters.Origin = fmt.Sprintf("%v", v)
		}
		if v, ok := req.Filters["date_from"]; ok {
			if f, ok := parseInt64(fmt.Sprintf("%v", v)); ok {
				searchReq.Filters.DateFrom = f
			}
		}
		if v, ok := req.Filters["date_to"]; ok {
			if f, ok := parseInt64(fmt.Sprintf("%v", v)); ok {
				searchReq.Filters.DateTo = f
			}
		}
	}

	chunks, err := h.vec.SearchChunks(ctx, searchReq)
	if err != nil {
		metrics.QueryTotal.WithLabelValues(hybrid, rerank, "search_error").Inc()
		return "", fmt.Errorf("向量检索失败: %w", err)
	}
	if chunks == nil {
		chunks = []model.RetrievedChunk{}
	}

	if h.reranker != nil && len(chunks) > 0 && req.Rerank {
		rerankTopK := req.RerankTopK
		if rerankTopK <= 0 {
			rerankTopK = 20
		}
		fetchTopK := rerankTopK
		if fetchTopK > len(chunks) {
			fetchTopK = len(chunks)
		}
		reranked, err := h.reranker.Rerank(ctx, effectiveQuery, chunks[:fetchTopK])
		if err != nil {
			log.Printf("[QueryHandler] rerank failed: %v", err)
		} else {
			chunks = reranked
		}
	}

	n := len(chunks)
	metrics.QueryChunksReturned.WithLabelValues(hybrid).Observe(float64(n))
	metrics.QueryTotal.WithLabelValues(hybrid, rerank, "success").Inc()
	log.Printf("[Query] user=%s query=%q hybrid=%v rerank=%v results=%d",
		req.UserID, effectiveQuery, req.Hybrid, req.Rerank, n)

	// 生成摘要：拼接 chunk 内容作为上下文
	return h.buildSummary(ctx, effectiveQuery, chunks)
}

// buildSummary 将 chunks 拼接为摘要字符串
func (h *QueryHandler) buildSummary(ctx context.Context, query string, chunks []model.RetrievedChunk) (string, error) {
	if len(chunks) == 0 {
		return "未找到相关内容。", nil
	}
	var sb strings.Builder
	for i, c := range chunks {
		if i > 0 {
			sb.WriteString("\n---\n")
		}
		sb.WriteString(c.Content)
		if i >= 5 {
			break
		}
	}
	return sb.String(), nil
}

// sendVoiceCallback 异步回调 voice-agent 的 ppt_message 端点
func (h *QueryHandler) sendVoiceCallback(sessionID, summary string, err error) {
	if h.voiceCBURL == "" {
		log.Printf("[QueryHandler] no voice callback URL configured, skipping callback session=%s", sessionID)
		return
	}
	summaryField := summary
	if err != nil {
		summaryField = fmt.Sprintf("检索失败: %v", err)
	}
	body := gin.H{
		"task_id":  sessionID,
		"msg_type": "kb_result",
		"summary":  summaryField,
	}
	payload, _ := json.Marshal(body)
	resp, err := h.httpClient.Post(h.voiceCBURL, "application/json", bytes.NewReader(payload))
	if err != nil {
		log.Printf("[QueryHandler] voice callback failed session=%s: %v", sessionID, err)
		return
	}
	defer resp.Body.Close()
	log.Printf("[QueryHandler] voice callback sent session=%s status=%d", sessionID, resp.StatusCode)
}

// QueryChunks POST /api/v1/kb/query-chunks
// 关键词检索：记忆模块 / PPT Agent 调用，同步返回 chunk 列表
// 传 user_id → 同时检索用户个人知识库；不传 → 仅检索专业知识库
func (h *QueryHandler) QueryChunks(c *gin.Context) {
	var req model.QueryChunksRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		util.Fail(c, util.CodeParamError, "请求体解析失败: "+err.Error())
		return
	}
	if len(req.Keywords) == 0 {
		util.Fail(c, util.CodeParamError, "keywords 不能为空")
		return
	}

	// BUG 1.2 修复：top_k 必须 > 0
	if req.TopK <= 0 {
		util.Fail(c, util.CodeParamError, "top_k 必须大于 0")
		return
	}
	if req.TopK > 20 {
		req.TopK = 20
	}
	// BUG 1.3 修复：score_threshold=0.0 表示不设阈值，不再静默覆盖
	if req.ScoreThreshold > 1.0 {
		util.Fail(c, util.CodeParamError, "score_threshold 不能大于 1.0")
		return
	}

	// ── 关键词分词 + BM25 评分 ──────────────────────────────────────────────
	var allTokens []string
	for _, kw := range req.Keywords {
		allTokens = append(allTokens, queryTokenize(kw)...)
	}
	if len(allTokens) == 0 {
		allTokens = req.Keywords
	}

	bm := newQueryBM25(allTokens)
	sparseIndices, sparseValues := bm.Score(allTokens)

	searchReq := store.SearchChunksReq{
		SparseVector:   sparseIndices,
		SparseValues:   sparseValues,
		QueryText:      strings.Join(req.Keywords, " "),
		UserID:         req.UserID,
		CollectionID:   req.CollectionID,
		TopK:           req.TopK,
		ScoreThreshold: req.ScoreThreshold,
		Hybrid:         true, // 混合模式，dense=0 让 BM25 主导
		DenseWeight:    0,    // dense 权重=0，完全依赖 BM25
	}

	chunks, err := h.vec.SearchChunks(c.Request.Context(), searchReq)
	if err != nil {
		util.Fail(c, util.CodeDependencyUnavailable, "关键词检索失败: "+err.Error())
		return
	}
	if chunks == nil {
		chunks = []model.RetrievedChunk{}
	}

	n := len(chunks)
	metrics.QueryChunksReturned.WithLabelValues("keyword").Observe(float64(n))
	metrics.QueryTotal.WithLabelValues("keyword", "false", "success").Inc()
	log.Printf("[QueryChunks] keywords=%v user=%s topk=%d results=%d",
		req.Keywords, req.UserID, req.TopK, n)

	util.OK(c, model.QueryChunksResponse{
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
