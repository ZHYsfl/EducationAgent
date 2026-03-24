// Package parser 实现第8章参考资料处理流水线的核心接口。
package parser

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"kb-service/internal/model"
	"kb-service/pkg/util"
)

// ChunkConfig 分块配置（见规范 §8.4）
type ChunkConfig struct {
	ChunkSize    int
	ChunkOverlap int
	MinChunkSize int
}

// DefaultChunkConfig 默认分块参数
func DefaultChunkConfig() ChunkConfig {
	return ChunkConfig{
		ChunkSize:    800,
		ChunkOverlap: 100,
		MinChunkSize: 100,
	}
}

// Parser 文档解析器接口（§8.1）
type Parser interface {
	Parse(ctx context.Context, input model.ParseInput) (*model.ParsedDocument, error)
	SupportedTypes() []string
}

// SimpleParser 简单文本解析器（happy-path 实现）
// 对于 text/web_snippet 直接在 Go 中切块；
// 对于 pdf/docx/pptx/image/video 调用外部 Python 解析服务（可选）。
type SimpleParser struct {
	cfg              ChunkConfig
	pythonServiceURL string // 可选：外部解析服务地址，如 http://localhost:8888
}

// NewSimpleParser 创建解析器
func NewSimpleParser(pythonServiceURL string) *SimpleParser {
	return &SimpleParser{
		cfg:              DefaultChunkConfig(),
		pythonServiceURL: pythonServiceURL,
	}
}

func (p *SimpleParser) SupportedTypes() []string {
	return []string{"text", "web_snippet", "pdf", "docx", "pptx", "image", "video"}
}

// Parse 解析文档并返回 ParsedDocument。
func (p *SimpleParser) Parse(ctx context.Context, input model.ParseInput) (*model.ParsedDocument, error) {
	switch input.FileType {
	case "text", "web_snippet":
		return p.parseText(input)
	default:
		if p.pythonServiceURL != "" {
			return p.parseViaExternalService(ctx, input)
		}
		// 无外部服务时降级占位
		return p.parseText(input)
	}
}

// parseText 对纯文本按 ChunkConfig 切块
func (p *SimpleParser) parseText(input model.ParseInput) (*model.ParsedDocument, error) {
	// web_snippet 场景：优先使用 Content 字段；普通文本退回 FileURL
	content := input.Content
	if content == "" {
		content = input.FileURL
	}
	chunks := splitIntoChunks(content, input.DocID, p.cfg)
	return &model.ParsedDocument{
		DocID:      input.DocID,
		FileType:   input.FileType,
		Title:      titleFromContent(content),
		TextChunks: chunks,
		Summary:    summarize(content),
	}, nil
}

// parseViaExternalService 调用外部 Python 解析微服务
// 联调时替换为真实 HTTP 调用；此处返回占位结果保证 happy-path 可通过。
func (p *SimpleParser) parseViaExternalService(_ context.Context, input model.ParseInput) (*model.ParsedDocument, error) {
	chunkID := util.NewID("chunk_")
	return &model.ParsedDocument{
		DocID:    input.DocID,
		FileType: input.FileType,
		Title:    fmt.Sprintf("%s 文档", input.FileType),
		TextChunks: []model.TextChunk{
			{
				ChunkID: chunkID,
				DocID:   input.DocID,
				Content: fmt.Sprintf("[待解析] 文件 %s 需 Python 服务处理", input.FileURL),
				Metadata: model.ChunkMeta{
					ChunkIndex: 0,
					SourceType: "text",
				},
			},
		},
		Summary: "待 Python 解析服务处理",
	}, nil
}

// ── 切块辅助函数 ──────────────────────────────────────────────────────────────

// splitIntoChunks 按段落优先、字符数兜底的分块策略。
func splitIntoChunks(text, docID string, cfg ChunkConfig) []model.TextChunk {
	if text == "" {
		return nil
	}
	// 先按段落分割
	paras := splitByParagraph(text)
	var chunks []model.TextChunk
	var buf strings.Builder
	chunkIdx := 0
	start := 0

	flush := func() {
		s := buf.String()
		if utf8.RuneCountInString(s) < cfg.MinChunkSize {
			return
		}
		chunks = append(chunks, model.TextChunk{
			ChunkID: util.NewID("chunk_"),
			DocID:   docID,
			Content: strings.TrimSpace(s),
			Metadata: model.ChunkMeta{
				ChunkIndex: chunkIdx,
				StartChar:  start,
				EndChar:    start + len(s),
				SourceType: "text",
			},
		})
		chunkIdx++
		// 保留 overlap
		runes := []rune(s)
		overlapStart := len(runes) - cfg.ChunkOverlap
		if overlapStart < 0 {
			overlapStart = 0
		}
		overlap := string(runes[overlapStart:])
		start += len(s) - len(overlap)
		buf.Reset()
		buf.WriteString(overlap)
	}

	for _, para := range paras {
		if utf8.RuneCountInString(buf.String())+utf8.RuneCountInString(para) > cfg.ChunkSize {
			flush()
		}
		if buf.Len() > 0 {
			buf.WriteString("\n")
		}
		buf.WriteString(para)
	}
	flush()
	return chunks
}

func splitByParagraph(text string) []string {
	lines := strings.Split(text, "\n")
	var paras []string
	var buf strings.Builder
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			if buf.Len() > 0 {
				paras = append(paras, buf.String())
				buf.Reset()
			}
		} else {
			if buf.Len() > 0 {
				buf.WriteString(" ")
			}
			buf.WriteString(strings.TrimSpace(line))
		}
	}
	if buf.Len() > 0 {
		paras = append(paras, buf.String())
	}
	return paras
}

func summarize(content string) string {
	runes := []rune(content)
	if len(runes) <= 200 {
		return content
	}
	return string(runes[:200]) + "..."
}

func titleFromContent(content string) string {
	lines := strings.Split(strings.TrimSpace(content), "\n")
	if len(lines) > 0 && len(lines[0]) > 0 {
		t := strings.TrimSpace(lines[0])
		if utf8.RuneCountInString(t) > 50 {
			return string([]rune(t)[:50]) + "..."
		}
		return t
	}
	return "未命名文档"
}
