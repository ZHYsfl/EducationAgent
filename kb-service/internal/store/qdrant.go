// Package store Qdrant 向量层实现 VecStore 接口。
//
// 依赖：Qdrant（docker: qdrant/qdrant，端口 6333 HTTP）
// 使用 HTTP REST API，无需额外 gRPC 依赖。
//
// Collection 命名：kb_chunks（全局单集合，通过 payload 过滤 user_id/collection_id）
//
// 混合检索实现：
//   1. Dense 向量：bge-m3 1024维，HNSW 索引，余弦相似度
//   2. Sparse 向量：BM25 权重，RRF 融合（reciprocal rank fusion）
//   3. 预计算 BM25：在 UpsertChunks 时计算并存储，避免查询时重复计算
package store

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"kb-service/internal/model"
)

const (
	qdrantCollection = "kb_chunks"
	vecDimQdrant     = 1024 // BAAI/bge-m3 维度
	sparseVecName    = "bm25"
)

// ── BM25 计算器（在写入时预计算，查询时直接使用）─────────────────────────────

var (
	// 中文分词（简单按字符 + 词边界切分）
	chineseWordRE = regexp.MustCompile(`[\p{Han}]+`)
	asciiWordRE   = regexp.MustCompile(`[A-Za-z0-9_]+`)
)

// BM25 轻量级实现，用于写入时预计算每个 chunk 的 sparse vector
type BM25 struct {
	avgDL  float64
	docs   [][]string
	idfMap map[string]float64
	k1     float64
	b      float64
}

func NewBM25(docs [][]string) *BM25 {
	bm := &BM25{k1: 1.5, b: 0.75, docs: docs}
	if len(docs) == 0 {
		bm.avgDL = 0
		return bm
	}
	var total int
	for _, doc := range docs {
		total += len(doc)
	}
	bm.avgDL = float64(total) / float64(len(docs))
	bm.idfMap = bm.computeIDF()
	return bm
}

// computeIDF 计算每个 term 的 IDF 值
func (bm *BM25) computeIDF() map[string]float64 {
	dfMap := make(map[string]int) // term -> 包含该 term 的文档数
	for _, doc := range bm.docs {
		seen := make(map[string]bool)
		for _, term := range doc {
			if !seen[term] {
				dfMap[term]++
				seen[term] = true
			}
		}
	}
	N := float64(len(bm.docs))
	idf := make(map[string]float64)
	for term, df := range dfMap {
		idf[term] = math.Log((N - float64(df) + 0.5) / (float64(df) + 0.5))
	}
	return idf
}

// Score 返回 doc 中每个 term 的 BM25 权重
func (bm *BM25) Score(doc []string) []uint32 {
	freqMap := make(map[string]float64)
	for _, term := range doc {
		freqMap[term]++
	}
	dl := float64(len(doc))
	var indices []uint32
	var values []float64
	idx := uint32(0)
	for _, term := range doc {
		if _, seen := freqMap[term]; !seen {
			idx++
			continue
		}
		idf := bm.idfMap[term]
		if idf <= 0 {
			idf = 0.1
		}
		tf := freqMap[term]
		norm := tf * (bm.k1 + 1) / (tf + bm.k1*(1-bm.b+b*(dl/bm.avgDL)))
		score := idf * norm
		if score > 0 {
			indices = append(indices, idx)
			values = append(values, score)
		}
		idx++
	}
	if len(indices) == 0 {
		return nil
	}
	// 排序并保留 top 64 项
	type kv struct{ idx uint32; val float64 }
	kvs := make([]kv, len(indices))
	for i := range indices {
		kvs[i] = kv{indices[i], values[i]}
	}
	sort.Slice(kvs, func(i, j int) bool { return kvs[i].val > kvs[j].val })
	if len(kvs) > 64 {
		kvs = kvs[:64]
	}
	result := make([]uint32, len(kvs)*2)
	for i, kv := range kvs {
		result[i*2] = kv.idx
		result[i*2+1] = math.Float32bits(float32(kv.val))
	}
	return result
}

// Tokenize 文本分词（中文按字符 + 连续汉字词，英文按 token）
func Tokenize(text string) []string {
	var tokens []string
	seen := make(map[string]bool)

	// 中文：按连续汉字词提取
	for _, match := range chineseWordRE.FindAllString(text, -1) {
		for _, r := range match {
			t := strings.ToLower(string(r))
			if !seen[t] {
				tokens = append(tokens, t)
				seen[t] = true
			}
		}
		// 2-gram 词
		rs := []rune(match)
		for i := 0; i < len(rs)-1; i++ {
			t := strings.ToLower(string(rs[i]) + string(rs[i+1]))
			if !seen[t] {
				tokens = append(tokens, t)
				seen[t] = true
			}
		}
	}

	// 英文/数字 token
	for _, match := range asciiWordRE.FindAllString(text, -1) {
		t := strings.ToLower(match)
		if len(t) < 2 {
			continue
		}
		if !seen[t] {
			tokens = append(tokens, t)
			seen[t] = true
		}
	}

	// 去除停用词（常见中文停用词）
	stopWords := map[string]bool{
		"的": true, "了": true, "是": true, "在": true, "和": true, "与": true,
		"或": true, "的": true, "而": true, "及": true, "等": true, "对": true,
		"于": true, "为": true, "以": true, "有": true, "这": true, "那": true,
		"the": true, "a": true, "an": true, "of": true, "in": true, "to": true,
		"and": true, "is": true, "for": true, "on": true, "with": true, "as": true,
		"by": true, "at": true, "from": true, "that": true, "this": true,
	}
	var filtered []string
	for _, t := range tokens {
		if !stopWords[t] {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

// ── QdrantStore ─────────────────────────────────────────────────────────────

// QdrantStore Qdrant 向量检索层
type QdrantStore struct {
	baseURL string // 如 http://localhost:6333
	client  *http.Client
}

// NewQdrantStore 创建 Qdrant 客户端并确保集合存在
func NewQdrantStore(baseURL string) (*QdrantStore, error) {
	qs := &QdrantStore{
		baseURL: baseURL,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
	if err := qs.ensureCollection(context.Background()); err != nil {
		return nil, fmt.Errorf("ensure qdrant collection: %w", err)
	}
	return qs, nil
}

func (q *QdrantStore) Close() error { return nil }

// ensureCollection 幂等创建向量集合（HNSW + Cosine，支持 sparse vector）
func (q *QdrantStore) ensureCollection(ctx context.Context) error {
	resp, err := q.do(ctx, http.MethodGet, fmt.Sprintf("/collections/%s", qdrantCollection), nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return nil
	}

	// 创建集合：同时启用 dense 向量和 sparse 向量
	body := map[string]any{
		"vectors": map[string]any{
			"size":     vecDimQdrant,
			"distance": "Cosine",
			"hnsw_config": map[string]any{
				"m":                  16,
				"ef_construct":       100,
				"full_scan_threshold": 10000,
			},
		},
		"sparse_vectors": map[string]any{
			sparseVecName: map[string]any{},
		},
	}
	resp2, err := q.do(ctx, http.MethodPut, fmt.Sprintf("/collections/%s", qdrantCollection), body)
	if err != nil {
		return err
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp2.Body)
		return fmt.Errorf("create collection status %d: %s", resp2.StatusCode, b)
	}
	return nil
}

// qdrantPointID strips "chunk_" prefix
func qdrantPointID(chunkID string) string {
	return strings.TrimPrefix(chunkID, "chunk_")
}

// UpsertChunks 批量写入向量 chunk（自动计算并存储 BM25 sparse vector）
func (q *QdrantStore) UpsertChunks(ctx context.Context, chunks []ChunkVector) error {
	if len(chunks) == 0 {
		return nil
	}

	// ── 批量计算 BM25 ────────────────────────────────────────────────────
	docs := make([][]string, len(chunks))
	for i, c := range chunks {
		docs[i] = Tokenize(c.Content)
	}
	bm25 := NewBM25(docs)

	points := make([]map[string]any, 0, len(chunks))
	for i, c := range chunks {
		sparse := bm25.Score(docs[i])
		pt := map[string]any{
			"id":     qdrantPointID(c.ChunkID),
			"vector": c.Vector,
		}
		// 写入 BM25 sparse vector（格式：[idx1, bits1, idx2, bits2, ...]）
		if len(sparse) > 0 {
			sparseMap := parseSparseVec(sparse)
			pt["sparse_vectors"] = map[string]any{
				sparseVecName: sparseMap,
			}
		}
		pt["payload"] = map[string]any{
			"doc_id":        c.DocID,
			"doc_title":     c.DocTitle,
			"collection_id": c.CollectionID,
			"user_id":       c.UserID,
			"content":       c.Content,
			"source_type":   c.Metadata.SourceType,
			"chunk_index":   c.Metadata.ChunkIndex,
			"page_number":   c.Metadata.PageNumber,
			"section_title": c.Metadata.SectionTitle,
			"image_url":     c.Metadata.ImageURL,
			"origin":        c.Metadata.Origin,
			"source_url":    c.Metadata.SourceURL,
			"created_at":    c.Metadata.CreatedAt,
		}
		points = append(points, pt)
	}

	body := map[string]any{"points": points}
	resp, err := q.do(ctx, http.MethodPut,
		fmt.Sprintf("/collections/%s/points?wait=true", qdrantCollection), body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("upsert points status %d: %s", resp.StatusCode, b)
	}
	return nil
}

// parseSparseVec 将 []uint32{idx1, bits1, idx2, bits2, ...} 转为 Qdrant sparse vector 格式
func parseSparseVec(data []uint32) map[string]any {
	indices := make([]uint32, 0, len(data)/2)
	values := make([]float32, 0, len(data)/2)
	for i := 0; i < len(data); i += 2 {
		indices = append(indices, data[i])
		values = append(values, math.Float32frombits(data[i+1]))
	}
	return map[string]any{
		"indices": indices,
		"values":  values,
	}
}

// SearchChunks 语义检索（支持纯向量 / 混合检索 / 过滤）
// 混合检索：RRF(reciprocal rank fusion) 融合 dense 向量检索结果 + 本地 BM25 关键词重排
func (q *QdrantStore) SearchChunks(ctx context.Context, req SearchChunksReq) ([]model.RetrievedChunk, error) {
	topK := req.TopK
	if topK <= 0 {
		topK = 5
	}
	if topK > 20 {
		topK = 20
	}
	// 混合检索时多取一些，融合后再截断
	searchLimit := topK * 4

	filter := q.buildFilter(req)

	// ── 纯向量检索 ──────────────────────────────────────────────────────────
	if !req.Hybrid || len(req.Vector) == 0 {
		return q.vectorOnlySearch(ctx, req.Vector, searchLimit, req.ScoreThreshold, filter)
	}

	// ── 混合检索：Dense 检索 → 本地 BM25 关键词评分 → RRF 融合 ──────────────
	denseWeight := req.DenseWeight
	if denseWeight == 0 {
		denseWeight = 0.5
	}
	sparseWeight := float32(1.0) - denseWeight

	// 1. Dense 向量检索（取结果 + payload，用于后续本地 BM25 评分）
	denseChunks, err := q.vectorOnlySearch(ctx, req.Vector, searchLimit, 0, filter)
	if err != nil {
		return nil, fmt.Errorf("hybrid dense search: %w", err)
	}
	if len(denseChunks) == 0 {
		return nil, nil
	}

	// 2. 本地 BM25 关键词评分：对 dense 结果按 query 分词做 TF-IDF 重排
	queryTokens := Tokenize(req.QueryText)
	var bm25Scores []float64
	if len(queryTokens) > 0 {
		bm25Scores = localBM25Score(denseChunks, queryTokens)
	} else {
		bm25Scores = make([]float64, len(denseChunks))
	}

	// 3. RRF 融合：按 RRF 分数降序排列
	fused := rrfFusionDenseBM25(denseChunks, bm25Scores, denseWeight, sparseWeight)

	// 4. 取 topK 并填充
	if len(fused) > topK {
		fused = fused[:topK]
	}
	return fused, nil
}

// localBM25Score 对 denseChunks 中的每个 chunk，用 queryTokens 做本地 BM25 评分
func localBM25Score(chunks []model.RetrievedChunk, queryTokens []string) []float64 {
	if len(chunks) == 0 || len(queryTokens) == 0 {
		return make([]float64, len(chunks))
	}

	// 统计 query 中各 term 的出现频率
	qFreq := make(map[string]int)
	for _, t := range queryTokens {
		qFreq[t]++
	}

	N := float64(len(chunks))
	avgDL := 0.0
	docLens := make([]int, len(chunks))
	for i, c := range chunks {
		docLens[i] = len(c.Content)
		avgDL += float64(docLens[i])
	}
	avgDL /= N

	// 估算 IDF：统计每个 query term 在多少 chunk 中出现
	idfMap := make(map[string]float64)
	for term := range qFreq {
		df := 0
		for _, c := range chunks {
			if strings.Contains(c.Content, term) {
				df++
			}
		}
		dfF := float64(df)
		if dfF > 0 {
			idfMap[term] = math.Log((N - dfF + 0.5) / (dfF + 0.5))
		} else {
			idfMap[term] = 0.1
		}
	}

	k1, b := 1.5, 0.75
	scores := make([]float64, len(chunks))
	for i, c := range chunks {
		dl := float64(docLens[i])
		var score float64
		for term, tf := range qFreq {
			idf := idfMap[term]
			// 统计 term 在 chunk 中的出现次数
			chunkTF := float64(strings.Count(c.Content, term))
			norm := chunkTF * (k1 + 1) / (chunkTF + k1*(1-b+b*(dl/avgDL)))
			score += idf * norm
		}
		scores[i] = score
	}
	return scores
}

// rrfFusionDenseBM25 对 dense 结果和本地 BM25 分数做 RRF 融合，返回排序后的 chunk
func rrfFusionDenseBM25(chunks []model.RetrievedChunk, bm25Scores []float64, denseWeight, sparseWeight float32) []model.RetrievedChunk {
	if len(chunks) == 0 {
		return nil
	}
	if len(bm25Scores) != len(chunks) {
		bm25Scores = make([]float64, len(chunks))
	}

	k := 60.0
	type scoredChunk struct {
		chunk model.RetrievedChunk
		score float64
	}
	var scored []scoredChunk
	for i, c := range chunks {
		// Dense rank = i+1（已按 cosine 分数降序）
		denseRRF := denseWeight / (k + float64(i+1))
		// BM25 rank：按 bm25 分数降序，取 rank 为该 chunk 在降序排列中的位置
		bm25Rank := float64(findBM25Rank(bm25Scores, i))
		sparseRRF := sparseWeight / (k + bm25Rank)
		fused := denseRRF + sparseRRF
		scored = append(scored, scoredChunk{chunk: c, score: fused})
	}

	sort.Slice(scored, func(i, j int) bool { return scored[i].score > scored[j].score })
	result := make([]model.RetrievedChunk, len(scored))
	for i, s := range scored {
		result[i] = s.chunk
		result[i].Score = s.score // 覆盖为融合分数
	}
	return result
}

// findBM25Rank 返回 chunkIdx 在 bm25Scores 降序排列中的 rank（从1开始）
func findBM25Rank(scores []float64, chunkIdx int) int {
	target := scores[chunkIdx]
	higher := 0
	for _, s := range scores {
		if s > target {
			higher++
		}
	}
	return higher + 1
}

// sparseSearch 已废弃，混合检索改用本地 BM25 评分
func (q *QdrantStore) sparseSearch(ctx context.Context, indices []uint32, values []float32, limit int, scoreThreshold float64, filter []map[string]any) ([]string, []float64, error) {
	return nil, nil, nil
}

// buildFilter 从请求构建 Qdrant filter
func (q *QdrantStore) buildFilter(req SearchChunksReq) []map[string]any {
	filterMust := []map[string]any{
		{"key": "user_id", "match": map[string]any{"value": req.UserID}},
	}
	if req.CollectionID != "" {
		filterMust = append(filterMust, map[string]any{
			"key": "collection_id", "match": map[string]any{"value": req.CollectionID},
		})
	}
	// source_type 过滤
	if req.Filters.SourceType != "" {
		filterMust = append(filterMust, map[string]any{
			"key": "source_type", "match": map[string]any{"value": req.Filters.SourceType},
		})
	}
	// origin 过滤
	if req.Filters.Origin != "" {
		filterMust = append(filterMust, map[string]any{
			"key": "origin", "match": map[string]any{"value": req.Filters.Origin},
		})
	}
	// 日期范围过滤
	if req.Filters.DateFrom > 0 || req.Filters.DateTo > 0 {
		dateFilter := map[string]any{"key": "created_at", "range": map[string]any{}}
		rf := dateFilter["range"].(map[string]any)
		if req.Filters.DateFrom > 0 {
			rf["gte"] = float64(req.Filters.DateFrom)
		}
		if req.Filters.DateTo > 0 {
			rf["lte"] = float64(req.Filters.DateTo)
		}
		filterMust = append(filterMust, dateFilter)
	}
	return filterMust
}

// vectorOnlySearch 纯向量检索
func (q *QdrantStore) vectorOnlySearch(ctx context.Context, vector []float32, limit int, scoreThreshold float64, filter []map[string]any) ([]model.RetrievedChunk, error) {
	body := map[string]any{
		"vector":          vector,
		"limit":           limit,
		"with_payload":    true,
		"filter":          map[string]any{"must": filter},
		"score_threshold": scoreThreshold,
	}
	resp, err := q.do(ctx, http.MethodPost,
		fmt.Sprintf("/collections/%s/points/search", qdrantCollection), body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("search status %d: %s", resp.StatusCode, b)
	}
	return q.decodeSearchResult(resp)
}

// denseSearch 纯 dense 向量检索（内部用，返回 id 和 score）
func (q *QdrantStore) denseSearch(ctx context.Context, vector []float32, limit int, scoreThreshold float64, filter []map[string]any) ([]string, []float64, error) {
	body := map[string]any{
		"vector":          vector,
		"limit":            limit,
		"with_payload":     false,
		"with_vectors":     false,
		"filter":           map[string]any{"must": filter},
		"score_threshold":  scoreThreshold,
	}
	resp, err := q.do(ctx, http.MethodPost,
		fmt.Sprintf("/collections/%s/points/search", qdrantCollection), body)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, nil, fmt.Errorf("dense search status %d: %s", resp.StatusCode, b)
	}
	var result struct {
		Result []struct {
			ID    any      `json:"id"`
			Score float64  `json:"score"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, nil, err
	}
	ids := make([]string, len(result.Result))
	scores := make([]float64, len(result.Result))
	for i, r := range result.Result {
		ids[i] = fmt.Sprintf("%v", r.ID)
		scores[i] = r.Score
	}
	return ids, scores, nil
}

// sparseSearch 纯 sparse 向量检索（内部用）
func (q *QdrantStore) sparseSearch(ctx context.Context, indices []uint32, values []float32, limit int, scoreThreshold float64, filter []map[string]any) ([]string, []float64, error) {
	if len(indices) == 0 || len(values) == 0 {
		return nil, nil, nil
	}
	sparseVec := map[string]any{
		"indices": indices,
		"values":  values,
	}
	body := map[string]any{
		"vector":          map[string]any{sparseVecName: sparseVec},
		"limit":           limit,
		"with_payload":    false,
		"with_vectors":    false,
		"filter":          map[string]any{"must": filter},
		"score_threshold": scoreThreshold,
	}
	resp, err := q.do(ctx, http.MethodPost,
		fmt.Sprintf("/collections/%s/points/search", qdrantCollection), body)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, nil, fmt.Errorf("sparse search status %d: %s", resp.StatusCode, b)
	}
	var result struct {
		Result []struct {
			ID    any      `json:"id"`
			Score float64  `json:"score"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, nil, err
	}
	ids := make([]string, len(result.Result))
	scores := make([]float64, len(result.Result))
	for i, r := range result.Result {
		ids[i] = fmt.Sprintf("%v", r.ID)
		scores[i] = r.Score
	}
	return ids, scores, nil
}

// rrfFusion RRF 融合算法：RRF(d) = Σ 1/(k+rank(d))
// 返回 {id: fused_score}，按 score 降序
func (q *QdrantStore) rrfFusion(ids1 []string, scores1 []float64, ids2 []string, scores2 []float64, w1, w2 float32) map[string]float64 {
	k := 60.0
	rank1 := make(map[string]float64)
	for i, id := range ids1 {
		rank1[id] = 1.0 / (k + float64(i+1))
	}
	rank2 := make(map[string]float64)
	for i, id := range ids2 {
		rank2[id] = 1.0 / (k + float64(i+1))
	}

	// 合并所有 id
	allIDs := make(map[string]bool)
	for _, id := range ids1 {
		allIDs[id] = true
	}
	for _, id := range ids2 {
		allIDs[id] = true
	}

	fused := make(map[string]float64)
	for id := range allIDs {
		s := float64(w1)*rank1[id] + float64(w2)*rank2[id]
		fused[id] = s
	}
	return fused
}

// fillChunkDetails 根据 RRF 融合后的 id 列表，批量获取详细信息（payload）
func (q *QdrantStore) fillChunkDetails(ctx context.Context, fused map[string]float64, topK int, filter []map[string]any) ([]model.RetrievedChunk, error) {
	if len(fused) == 0 {
		return nil, nil
	}
	// 按 score 降序排列
	type kv struct{ id string; score float64 }
	sorted := make([]kv, 0, len(fused))
	for id, score := range fused {
		sorted = append(sorted, kv{id, score})
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].score > sorted[j].score })

	// 取 topK
	if len(sorted) > topK {
		sorted = sorted[:topK]
	}

	// 批量查询（Qdrant 每次最多 128 个 point）
	var allChunks []model.RetrievedChunk
	for i := 0; i < len(sorted); i += 128 {
		end := i + 128
		if end > len(sorted) {
			end = len(sorted)
		}
		batch := sorted[i:end]
		ids := make([]any, len(batch))
		for j, kv := range batch {
			ids[j] = kv.id
		}
		// 重建 id → score 映射
		scoreMap := make(map[string]float64)
		for _, kv := range batch {
			scoreMap[kv.id] = kv.score
		}

		reqBody := map[string]any{
			"ids":            ids,
			"with_payload":   true,
			"with_vectors":   false,
			"filter":         map[string]any{"must": filter},
		}
		resp, err := q.do(ctx, http.MethodPost,
			fmt.Sprintf("/collections/%s/points/retrieve", qdrantCollection), reqBody)
		if err != nil {
			return nil, err
		}
		var result struct {
			Result []struct {
				ID      any                    `json:"id"`
				Payload map[string]interface{} `json:"payload"`
			} `json:"result"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			resp.Body.Close()
			return nil, err
		}
		resp.Body.Close()

		for _, r := range result.Result {
			idStr := fmt.Sprintf("%v", r.ID)
			p := r.Payload
			allChunks = append(allChunks, model.RetrievedChunk{
				ChunkID:  idStr,
				DocID:    strVal(p, "doc_id"),
				DocTitle: strVal(p, "doc_title"),
				Content:  strVal(p, "content"),
				Score:    scoreMap[idStr],
				Metadata: model.ChunkMeta{
					SourceType:   strVal(p, "source_type"),
					ChunkIndex:   intVal(p, "chunk_index"),
					PageNumber:   intVal(p, "page_number"),
					SectionTitle: strVal(p, "section_title"),
					ImageURL:     strVal(p, "image_url"),
					Origin:       strVal(p, "origin"),
					SourceURL:    strVal(p, "source_url"),
					CreatedAt:    int64Val(p, "created_at"),
				},
			})
		}
	}
	return allChunks, nil
}

// decodeSearchResult 解码 Qdrant 检索结果
func (q *QdrantStore) decodeSearchResult(resp *http.Response) ([]model.RetrievedChunk, error) {
	var result struct {
		Result []struct {
			ID      any                    `json:"id"`
			Score   float64                `json:"score"`
			Payload map[string]interface{} `json:"payload"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode search result: %w", err)
	}
	chunks := make([]model.RetrievedChunk, 0, len(result.Result))
	for _, r := range result.Result {
		p := r.Payload
		chunks = append(chunks, model.RetrievedChunk{
			ChunkID:  fmt.Sprintf("%v", r.ID),
			DocID:    strVal(p, "doc_id"),
			DocTitle: strVal(p, "doc_title"),
			Content:  strVal(p, "content"),
			Score:    r.Score,
			Metadata: model.ChunkMeta{
				SourceType:   strVal(p, "source_type"),
				ChunkIndex:   intVal(p, "chunk_index"),
				PageNumber:   intVal(p, "page_number"),
				SectionTitle: strVal(p, "section_title"),
				ImageURL:     strVal(p, "image_url"),
				Origin:       strVal(p, "origin"),
				SourceURL:    strVal(p, "source_url"),
				CreatedAt:    int64Val(p, "created_at"),
			},
		})
	}
	return chunks, nil
}

// DeleteChunksByDocID 删除某文档的所有向量 chunk
func (q *QdrantStore) DeleteChunksByDocID(ctx context.Context, docID string) error {
	body := map[string]any{
		"filter": map[string]any{
			"must": []map[string]any{
				{"key": "doc_id", "match": map[string]any{"value": docID}},
			},
		},
	}
	resp, err := q.do(ctx, http.MethodPost,
		fmt.Sprintf("/collections/%s/points/delete?wait=true", qdrantCollection), body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete points status %d: %s", resp.StatusCode, b)
	}
	return nil
}

// ── HTTP 辅助 ─────────────────────────────────────────────────────────────────

func (q *QdrantStore) do(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, q.baseURL+path, bodyReader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return q.client.Do(req)
}

// strVal 安全从 payload map 取字符串值
func strVal(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// intVal 安全从 payload map 取整数值（Qdrant 数字返回 float64）
func intVal(m map[string]interface{}, key string) int {
	if v, ok := m[key]; ok {
		switch n := v.(type) {
		case float64:
			return int(n)
		case int:
			return n
		}
	}
	return 0
}

// int64Val 安全从 payload map 取 int64 值
func int64Val(m map[string]interface{}, key string) int64 {
	if v, ok := m[key]; ok {
		switch n := v.(type) {
		case float64:
			return int64(n)
		case int:
			return int64(n)
		case int64:
			return n
		}
	}
	return 0
}

// runeWordBoundary 判断 rune 是否为CJK字符（用于中文分词）
func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) ||
		unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) ||
		unicode.Is(unicode.Hangul, r)
}
