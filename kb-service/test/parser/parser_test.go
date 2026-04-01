// Package parser_test parser 层黑盒测试（仅测试公开接口）
package parser_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"kb-service/internal/model"
	"kb-service/internal/parser"
)

func TestSimpleParser_ParseText(t *testing.T) {
	p := parser.NewSimpleParser("")
	content := strings.Repeat("三角形内角和等于180度。这是基础几何定理。", 15)
	doc, err := p.Parse(context.Background(), model.ParseInput{
		Content:  content,
		FileType: "web_snippet",
		DocID:    "doc_001",
	})
	if err != nil {
		t.Fatalf("Parse 返回错误: %v", err)
	}
	if doc.DocID != "doc_001" {
		t.Errorf("DocID 不匹配")
	}
	if len(doc.TextChunks) == 0 {
		t.Errorf("期望至少 1 个 chunk")
	}
	if doc.Summary == "" {
		t.Errorf("Summary 不能为空")
	}
}

func TestSimpleParser_ContentPriority(t *testing.T) {
	p := parser.NewSimpleParser("")
	content := strings.Repeat("Content字段内容。", 20)
	doc, err := p.Parse(context.Background(), model.ParseInput{
		FileURL:  "http://example.com/file",
		Content:  content,
		FileType: "text",
		DocID:    "doc_priority",
	})
	if err != nil {
		t.Fatalf("Parse 返回错误: %v", err)
	}
	for _, c := range doc.TextChunks {
		if strings.Contains(c.Content, "example.com") {
			t.Errorf("chunk 不应包含 FileURL，应优先用 Content")
		}
	}
}

func TestSimpleParser_FallbackToFileURL(t *testing.T) {
	p := parser.NewSimpleParser("")
	content := strings.Repeat("通过FileURL传入的内容。", 20)
	doc, err := p.Parse(context.Background(), model.ParseInput{
		FileURL:  content,
		Content:  "",
		FileType: "text",
		DocID:    "doc_fallback",
	})
	if err != nil {
		t.Fatalf("Parse 返回错误: %v", err)
	}
	if len(doc.TextChunks) == 0 {
		t.Errorf("Content为空时应退回FileURL，期望至少 1 chunk")
	}
}

func TestSimpleParser_PDFFallback(t *testing.T) {
	p := parser.NewSimpleParser("")
	doc, err := p.Parse(context.Background(), model.ParseInput{
		FileURL:  "http://storage/file.pdf",
		FileType: "pdf",
		DocID:    "doc_pdf",
	})
	if err != nil {
		t.Fatalf("PDF 降级不应返回错误: %v", err)
	}
	if doc == nil {
		t.Fatal("降级结果不能为 nil")
	}
}

func TestSimpleParser_SupportedTypes(t *testing.T) {
	p := parser.NewSimpleParser("")
	types := p.SupportedTypes()
	required := []string{"text", "web_snippet", "pdf", "docx", "pptx", "image", "video"}
	typeSet := make(map[string]bool)
	for _, ft := range types {
		typeSet[ft] = true
	}
	for _, req := range required {
		if !typeSet[req] {
			t.Errorf("SupportedTypes 缺少类型: %s", req)
		}
	}
}

// ── MockEmbedder 测试 ─────────────────────────────────────────────────────────

func TestMockEmbedder(t *testing.T) {
	e := &parser.MockEmbedder{}
	vecs, err := e.Embed(context.Background(), []string{"hello", "world"})
	if err != nil {
		t.Fatalf("MockEmbedder.Embed 失败: %v", err)
	}
	if len(vecs) != 2 {
		t.Errorf("期望 2 个向量，得到 %d", len(vecs))
	}
	for i, v := range vecs {
		if len(v) == 0 {
			t.Errorf("向量[%d] 不能为空", i)
		}
	}
}

func TestMockEmbedder_EmptyInput(t *testing.T) {
	e := &parser.MockEmbedder{}
	vecs, err := e.Embed(context.Background(), []string{})
	if err != nil {
		t.Fatalf("空输入不应报错: %v", err)
	}
	if len(vecs) != 0 {
		t.Errorf("空输入期望 0 个向量，得到 %d", len(vecs))
	}
}

func TestMockEmbedder_VectorDimension(t *testing.T) {
	e := &parser.MockEmbedder{}
	vecs, _ := e.Embed(context.Background(), []string{"test"})
	if len(vecs[0]) != 1024 {
		t.Errorf("期望维度 1024，得到 %d", len(vecs[0]))
	}
}

func TestMockEmbedder_NonZeroVector(t *testing.T) {
	e := &parser.MockEmbedder{}
	vecs, _ := e.Embed(context.Background(), []string{"test"})
	allZero := true
	for _, v := range vecs[0] {
		if v != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Errorf("向量不应全为零（避免 Cosine 距离除零）")
	}
}

// ── HTTPEmbedder 测试 ─────────────────────────────────────────────────────────

func TestHTTPEmbedder_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/embed" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"embeddings":[[0.1,0.2,0.3],[0.4,0.5,0.6]]}`))
	}))
	defer server.Close()

	e := parser.NewHTTPEmbedder(server.URL)
	vecs, err := e.Embed(context.Background(), []string{"hello", "world"})
	if err != nil {
		t.Fatalf("HTTPEmbedder.Embed 失败: %v", err)
	}
	if len(vecs) != 2 {
		t.Errorf("期望 2 个向量，得到 %d", len(vecs))
	}
	if len(vecs[0]) != 3 {
		t.Errorf("期望维度 3，得到 %d", len(vecs[0]))
	}
}

func TestHTTPEmbedder_EmptyInput(t *testing.T) {
	e := parser.NewHTTPEmbedder("http://localhost:19999")
	vecs, err := e.Embed(context.Background(), []string{})
	if err != nil {
		t.Fatalf("空输入不应发请求，也不应报错: %v", err)
	}
	if vecs != nil {
		t.Errorf("空输入期望 nil，得到 %v", vecs)
	}
}

func TestHTTPEmbedder_ServiceError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	defer server.Close()

	e := parser.NewHTTPEmbedder(server.URL)
	_, err := e.Embed(context.Background(), []string{"test"})
	if err == nil {
		t.Errorf("服务返回 500 时应报错")
	}
}

func TestHTTPEmbedder_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not json`))
	}))
	defer server.Close()

	e := parser.NewHTTPEmbedder(server.URL)
	_, err := e.Embed(context.Background(), []string{"test"})
	if err == nil {
		t.Errorf("非法 JSON 响应应报错")
	}
}

func TestHTTPEmbedder_RequestPayload(t *testing.T) {
	var received []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Texts []string `json:"texts"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		received = req.Texts
		w.Header().Set("Content-Type", "application/json")
		// 返回与输入数量一致的向量
		parts := make([]string, len(req.Texts))
		for i := range parts {
			parts[i] = "[0.1]"
		}
		_, _ = fmt.Fprintf(w, `{"embeddings":[%s]}`, strings.Join(parts, ","))
	}))
	defer server.Close()

	e := parser.NewHTTPEmbedder(server.URL)
	_, _ = e.Embed(context.Background(), []string{"a", "b", "c"})
	if len(received) != 3 {
		t.Errorf("期望请求体包含 3 条文本，得到 %d", len(received))
	}
}

func TestHTTPEmbedder_ContentTypeHeader(t *testing.T) {
	var gotCT string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCT = r.Header.Get("Content-Type")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"embeddings":[[0.1]]}`))
	}))
	defer server.Close()

	e := parser.NewHTTPEmbedder(server.URL)
	_, _ = e.Embed(context.Background(), []string{"test"})
	if gotCT != "application/json" {
		t.Errorf("期望 Content-Type: application/json，得到: %s", gotCT)
	}
}

// ── 分块边界测试 ──────────────────────────────────────────────────────────────

func TestChunk_EmptyContent(t *testing.T) {
	p := parser.NewSimpleParser("")
	doc, err := p.Parse(context.Background(), model.ParseInput{
		Content: "", FileType: "text", DocID: "doc_empty",
	})
	if err != nil {
		t.Fatalf("空内容不应报错: %v", err)
	}
	if len(doc.TextChunks) != 0 {
		t.Errorf("空内容期望 0 个 chunk，得到 %d", len(doc.TextChunks))
	}
}

func TestChunk_ShortContent(t *testing.T) {
	p := parser.NewSimpleParser("")
	// 内容远小于 MinChunkSize(100)，应被丢弃
	doc, err := p.Parse(context.Background(), model.ParseInput{
		Content: "短文本", FileType: "text", DocID: "doc_short",
	})
	if err != nil {
		t.Fatalf("短内容不应报错: %v", err)
	}
	if len(doc.TextChunks) != 0 {
		t.Errorf("低于 MinChunkSize 的内容期望 0 个 chunk，得到 %d", len(doc.TextChunks))
	}
}

func TestChunk_OverlapBetweenChunks(t *testing.T) {
	p := parser.NewSimpleParser("")
	content := strings.Repeat("这是一段测试文字用于验证分块重叠逻辑是否正确工作。", 60)
	doc, err := p.Parse(context.Background(), model.ParseInput{
		Content: content, FileType: "text", DocID: "doc_overlap",
	})
	if err != nil {
		t.Fatalf("Parse 失败: %v", err)
	}
	if len(doc.TextChunks) < 2 {
		t.Skipf("内容不足以产生多个 chunk，跳过 overlap 测试")
	}
	// 相邻两个 chunk 末尾和开头应有重叠内容
	chunk0End := []rune(doc.TextChunks[0].Content)
	chunk1Start := []rune(doc.TextChunks[1].Content)
	overlapLen := 100
	if len(chunk0End) > overlapLen && len(chunk1Start) > overlapLen {
		tail := string(chunk0End[len(chunk0End)-overlapLen:])
		head := string(chunk1Start[:overlapLen])
		if tail != head {
			t.Errorf("相邻 chunk 应有 %d 字符重叠\ntail: %q\nhead: %q", overlapLen, tail, head)
		}
	}
}

func TestChunk_IndexIncrement(t *testing.T) {
	p := parser.NewSimpleParser("")
	content := strings.Repeat("知识点：微积分基本定理。", 80)
	doc, _ := p.Parse(context.Background(), model.ParseInput{
		Content: content, FileType: "text", DocID: "doc_idx",
	})
	for i, c := range doc.TextChunks {
		if c.Metadata.ChunkIndex != i {
			t.Errorf("chunk[%d].ChunkIndex = %d，期望 %d", i, c.Metadata.ChunkIndex, i)
		}
	}
}

func TestChunk_UniqueChunkIDs(t *testing.T) {
	p := parser.NewSimpleParser("")
	content := strings.Repeat("内容用于测试 chunk ID 唯一性。", 80)
	doc, _ := p.Parse(context.Background(), model.ParseInput{
		Content: content, FileType: "text", DocID: "doc_uid",
	})
	seen := make(map[string]bool)
	for _, c := range doc.TextChunks {
		if seen[c.ChunkID] {
			t.Errorf("ChunkID 重复: %s", c.ChunkID)
		}
		seen[c.ChunkID] = true
	}
}
