// Package parser_test parser 层黑盒测试（仅测试公开接口）
package parser_test

import (
	"context"
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
