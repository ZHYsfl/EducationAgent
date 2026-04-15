package service

import (
	"context"
	"fmt"
	"io/ioutil"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// Chunk 知识库检索结果块
type Chunk struct {
	ChunkID string `json:"chunk_id"`
	Content string `json:"content"`
}

// KBService 定义知识库查询接口
type KBService interface {
	QueryChunks(ctx context.Context, query string) ([]Chunk, int, error)
}

// DefaultKBService 本地文件检索实现
type DefaultKBService struct {
	basePath string
}

// NewKBService 创建本地知识库检索服务
func NewKBService() KBService {
	return &DefaultKBService{
		basePath: "new_implemention/data",
	}
}

// QueryChunks 检索知识库 chunks
// 实现 BM25 关键词检索算法，与 kb-service 逻辑一致
func (s *DefaultKBService) QueryChunks(ctx context.Context, query string) ([]Chunk, int, error) {
	// 1. 遍历知识库目录，收集所有 markdown 文件
	var files []string
	if err := filepath.Walk(s.basePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() && strings.HasSuffix(path, ".md") {
			files = append(files, path)
		}
		return nil
	}); err != nil {
		return nil, 0, fmt.Errorf("遍历知识库失败: %w", err)
	}

	// 2. 读取所有文件内容并分块
	type fileChunk struct {
		id      string
		content string
	}
	var allChunks []fileChunk
	for _, f := range files {
		content, err := ioutil.ReadFile(f)
		if err != nil {
			continue
		}

		blocks := splitIntoBlocks(string(content))
		for i, block := range blocks {
			if len(strings.TrimSpace(block)) < 20 {
				continue
			}
			allChunks = append(allChunks, fileChunk{
				id:      fmt.Sprintf("%s#%d", f, i),
				content: block,
			})
		}
	}

	if len(allChunks) == 0 {
		return nil, 0, nil
	}

	// 3. 对 query 进行分词（与 kb-service queryTokenize 一致）
	queryTokens := queryTokenize(query)
	if len(queryTokens) == 0 {
		return s.returnTopChunks(allChunks, 5)
	}

	// 4. 计算 BM25 分数（与 kb-service newQueryBM25 + Score 一致）
	bm := newQueryBM25(queryTokens)
	var scored []struct {
		chunk fileChunk
		score float64
	}
	for _, c := range allChunks {
		contentTokens := tokenize(c.content)
		if len(contentTokens) == 0 {
			continue
		}
		score := bm.ScoreDoc(queryTokens, contentTokens)
		if score > 0 {
			scored = append(scored, struct {
				chunk fileChunk
				score float64
			}{chunk: c, score: score})
		}
	}

	// 5. 按分数排序，取前 10 个
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})
	if len(scored) > 10 {
		scored = scored[:10]
	}

	// 6. 转换为返回格式
	result := make([]Chunk, 0, len(scored))
	for _, sc := range scored {
		result = append(result, Chunk{
			ChunkID: sc.chunk.id,
			Content: sc.chunk.content,
		})
	}
	return result, len(result), nil
}

// returnTopChunks 返回前 topK 个 chunk
func (s *DefaultKBService) returnTopChunks(chunks []fileChunk, topK int) ([]Chunk, int, error) {
	if len(chunks) > topK {
		chunks = chunks[:topK]
	}
	result := make([]Chunk, 0, len(chunks))
	for _, c := range chunks {
		result = append(result, Chunk{
			ChunkID: c.id,
			Content: c.content,
		})
	}
	return result, len(result), nil
}

// splitIntoBlocks 按 PAGE_DIV 将内容切分为块
func splitIntoBlocks(content string) []string {
	content = regexp.MustCompile(`(?s)^本文档为.*?\n`).ReplaceAllString(content, "")
	parts := strings.Split(content, "<PAGE_DIV>")
	var blocks []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			blocks = append(blocks, p)
		}
	}
	if len(blocks) == 0 && content != "" {
		blocks = append(blocks, content)
	}
	return blocks
}

// ── BM25 实现（与 kb-service query.go 一致）──────────────────────────────

// bm25Query BM25 查询评分器
type bm25Query struct {
	avgDL  float64
	idfMap map[string]float64
	k1     float64
	b      float64
}

func newQueryBM25(tokens []string) *bm25Query {
	bm := &bm25Query{k1: 1.5, b: 0.75}
	freq := make(map[string]int)
	for _, t := range tokens {
		freq[t]++
	}
	bm.avgDL = float64(len(tokens))
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

// ScoreDoc 计算单个文档的 BM25 分数
func (bm *bm25Query) ScoreDoc(queryTokens, docTokens []string) float64 {
	if len(docTokens) == 0 {
		return 0
	}
	freq := make(map[string]int)
	for _, t := range docTokens {
		freq[t]++
	}
	var score float64
	dl := float64(len(docTokens))
	for term, qtf := range freq {
		idf := bm.idfMap[term]
		if idf <= 0 {
			continue
		}
		tf := float64(qtf)
		norm := tf * (bm.k1 + 1) / (tf + bm.k1*(1-bm.b+bm.b*dl/bm.avgDL))
		score += idf * norm
	}
	return score
}

// queryTokenize 分词（与 kb-service queryTokenize 一致）
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
	// 中文：逐字符提取
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
	// 英文/数字：按非字母数字分割
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

// tokenize 文档分词（用于 BM25 评分）
func tokenize(text string) []string {
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
	// 中文：逐字符
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
	// 英文/数字
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
