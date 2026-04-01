package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	openai "github.com/openai/openai-go/v3"
	toolcalling "tool_calling_go"

	"zcxppt/internal/infra"
	"zcxppt/internal/model"
)

// RefFusionService orchestrates per-file parsing, targeted extraction, and style mapping
// for all reference files attached to a PPT init or feedback request.
type RefFusionService struct {
	parser    *infra.FileParser
	llmCfg    LLMClientConfig
	kbBaseURL string
}

func NewRefFusionService(parser *infra.FileParser, llmCfg LLMClientConfig, kbBaseURL string) *RefFusionService {
	return &RefFusionService{
		parser:    parser,
		llmCfg:    llmCfg,
		kbBaseURL: strings.TrimSpace(kbBaseURL),
	}
}

// FusionResult is the aggregated output of processing all reference files for a single page.
type FusionResult struct {
	// ExtractedContent contains per-file structured content relevant to this page's topic.
	ExtractedContent []ExtractedFileContent `json:"extracted_content"`
	// StyleGuide consolidates all style hints (colors, fonts, layouts) from reference PPTX files.
	StyleGuide StyleGuide `json:"style_guide"`
	// TopicHints are KB-enriched knowledge points derived from the reference materials.
	TopicHints []string `json:"topic_hints"`
}

// ExtractedFileContent is the parsed and LLM-extracted content from one reference file.
type ExtractedFileContent struct {
	FileID       string `json:"file_id"`
	FileType     string `json:"file_type"`
	Instruction  string `json:"instruction"`
	RawText      string `json:"raw_text,omitempty"`
	ExtractedText string  `json:"extracted_text,omitempty"`
	Summary      string `json:"summary,omitempty"`
}

// StyleGuide consolidates design tokens extracted from reference PPTX files.
type StyleGuide struct {
	ThemeColors []string          `json:"theme_colors,omitempty"`
	Fonts       []string          `json:"fonts,omitempty"`
	Layouts     []string          `json:"layouts,omitempty"`
	ColorHex    map[string]string `json:"color_hex,omitempty"`
}

// Fuse processes all ReferenceFiles attached to a PPTInitRequest and returns a FusionResult
// suitable for injecting into the LLM generation prompt.
//
// The pipeline is:
//  1. Batch-parse all files concurrently (PDF/DOCX/PPTX/Image/Video).
//  2. For each parsed result, run LLM-guided targeted extraction based on the file's instruction.
//  3. Consolidate style hints from PPTX files into a StyleGuide.
//  4. Optionally query KB for enrichment if topic is set.
func (s *RefFusionService) Fuse(ctx context.Context, req model.PPTInitRequest, pageIndex int, pageTopic string) (*FusionResult, error) {
	if len(req.ReferenceFiles) == 0 {
		return nil, nil
	}

	// Step 1: Batch parse all files concurrently
	parseResults := s.parser.ParseBatch(ctx, req.ReferenceFiles)

	result := &FusionResult{
		ExtractedContent: make([]ExtractedFileContent, 0, len(parseResults)),
		StyleGuide:       StyleGuide{ColorHex: make(map[string]string)},
		TopicHints:       []string{},
	}

	// Step 2: Per-file targeted extraction via LLM
	for _, pr := range parseResults {
		fileInstruction := lookupInstructionByFileID(req.ReferenceFiles, pr.FileID)
		efc := ExtractedFileContent{
			FileID:      pr.FileID,
			FileType:    pr.FileType,
			Instruction: fileInstruction,
			RawText:     pr.TextContent,
		}

		if pr.Error != "" {
			efc.ExtractedText = fmt.Sprintf("[解析失败] %s", pr.Error)
		} else {
			// LLM-guided targeted extraction: pull out only the content relevant to this page's topic
			if s.llmCfg.APIKey != "" && s.llmCfg.Model != "" && pr.TextContent != "" {
				extracted, err := s.extractRelevantContent(ctx, pr.TextContent, fileInstruction, pageTopic)
				if err == nil && extracted != "" {
					efc.ExtractedText = extracted
					efc.Summary = summarizeText(extracted, 200)
				} else {
					efc.ExtractedText = pr.TextContent
					efc.Summary = summarizeText(pr.TextContent, 200)
				}
			} else {
				efc.ExtractedText = pr.TextContent
				efc.Summary = summarizeText(pr.TextContent, 200)
			}
		}

		result.ExtractedContent = append(result.ExtractedContent, efc)

		// Step 3: Consolidate style hints from PPTX files
		if pr.FileType == "pptx" {
			for k, v := range pr.StyleHints {
				if k == "theme_color" || strings.Contains(k, "color") {
					result.StyleGuide.ThemeColors = append(result.StyleGuide.ThemeColors, v)
				}
				if k == "font" || strings.Contains(k, "font") {
					result.StyleGuide.Fonts = append(result.StyleGuide.Fonts, v)
				}
				if k == "layout" || strings.Contains(k, "layout") {
					result.StyleGuide.Layouts = append(result.StyleGuide.Layouts, v)
				}
				if strings.HasPrefix(k, "hex_") {
					result.StyleGuide.ColorHex[k] = v
				}
			}
		}
	}

	// Step 4: KB enrichment
	if s.kbBaseURL != "" {
		hints, _ := s.enrichFromKB(ctx, req.Topic, req.Subject)
		result.TopicHints = hints
	}

	return result, nil
}

// FuseForFeedback processes reference files for the feedback (modification) phase.
// Unlike init, it re-pulls reference content filtered against the feedback instruction.
func (s *RefFusionService) FuseForFeedback(ctx context.Context, refFiles []model.ReferenceFile, feedbackInstruction, pageTopic string) (*FusionResult, error) {
	if len(refFiles) == 0 {
		return nil, nil
	}

	parseResults := s.parser.ParseBatch(ctx, refFiles)

	result := &FusionResult{
		ExtractedContent: make([]ExtractedFileContent, 0, len(parseResults)),
		StyleGuide:       StyleGuide{ColorHex: make(map[string]string)},
		TopicHints:       []string{},
	}

	for _, pr := range parseResults {
		efc := ExtractedFileContent{
			FileID:      pr.FileID,
			FileType:    pr.FileType,
			Instruction: findInstruction(pr.Instruction, feedbackInstruction),
			RawText:     pr.TextContent,
		}

		if pr.Error != "" {
			efc.ExtractedText = fmt.Sprintf("[解析失败] %s", pr.Error)
		} else {
			if s.llmCfg.APIKey != "" && s.llmCfg.Model != "" && pr.TextContent != "" {
				extracted, err := s.extractRelevantContent(ctx, pr.TextContent, findInstruction(pr.Instruction, feedbackInstruction), pageTopic)
				if err == nil && extracted != "" {
					efc.ExtractedText = extracted
					efc.Summary = summarizeText(extracted, 200)
				} else {
					efc.ExtractedText = pr.TextContent
					efc.Summary = summarizeText(pr.TextContent, 200)
				}
			} else {
				efc.ExtractedText = pr.TextContent
				efc.Summary = summarizeText(pr.TextContent, 200)
			}
		}

		result.ExtractedContent = append(result.ExtractedContent, efc)

		if pr.FileType == "pptx" {
			for k, v := range pr.StyleHints {
				if strings.Contains(strings.ToLower(k), "color") {
					result.StyleGuide.ThemeColors = append(result.StyleGuide.ThemeColors, v)
				}
				if strings.Contains(strings.ToLower(k), "font") {
					result.StyleGuide.Fonts = append(result.StyleGuide.Fonts, v)
				}
				if strings.Contains(strings.ToLower(k), "layout") {
					result.StyleGuide.Layouts = append(result.StyleGuide.Layouts, v)
				}
			}
		}
	}

	return result, nil
}

// extractRelevantContent uses the LLM to extract only the content fragments from rawText
// that are relevant to the given instruction and target page topic.
func (s *RefFusionService) extractRelevantContent(ctx context.Context, rawText, instruction, pageTopic string) (string, error) {
	if rawText == "" || (instruction == "" && pageTopic == "") {
		return rawText, nil
	}

	system := `你是一个精准的内容抽取助手。请根据用户的定向抽取指令（instruction），从原文（raw_text）中精确抽取出与指令相关的段落和句子，返回抽取结果。不要总结或改写，只返回原文中最相关的片段。如果原文与指令无关，返回空字符串。

严格只输出抽取出的原文内容，不要添加任何解释、标签或JSON包装。`

	user := fmt.Sprintf("instruction=%s\npage_topic=%s\nraw_text=%s", instruction, pageTopic, rawText)

	agent := toolcalling.NewAgent(toolcalling.LLMConfig{
		APIKey:  s.llmCfg.APIKey,
		BaseURL: s.llmCfg.BaseURL,
		Model:   s.llmCfg.Model,
	})

	msgs := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(system),
		openai.UserMessage(user),
	}

	resp, err := agent.ChatText(ctx, msgs)
	if err != nil {
		return "", err
	}

	// Strip markdown code blocks if present
	cleaned := strings.TrimSpace(resp)
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	cleaned = strings.TrimSpace(cleaned)

	return cleaned, nil
}

// enrichFromKB queries the KB service for topic enrichment.
func (s *RefFusionService) enrichFromKB(ctx context.Context, topic, subject string) ([]string, error) {
	if topic == "" {
		return nil, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimSuffix(s.kbBaseURL, "/")+"/api/v1/kb/parse",
		strings.NewReader(fmt.Sprintf(`{"query":"%s","subject":"%s","top_k":5}`, topic, subject)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, nil
	}

	var env struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if json.Unmarshal(b, &env) == nil && env.Data != nil {
		var text string
		if json.Unmarshal(env.Data, &text) == nil {
			return strings.Split(summarizeText(text, 500), "。"), nil
		}
	}
	return nil, nil
}

// FusionResultToPrompt serializes a FusionResult into an LLM-prompt-friendly string
// that can be injected into the system prompt or user message.
func FusionResultToPrompt(r *FusionResult, pageIndex int, pageTopic string) string {
	if r == nil {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("\n[参考资料（第%d页: %s）]\n", pageIndex+1, pageTopic))

	if len(r.ExtractedContent) > 0 {
		for _, ec := range r.ExtractedContent {
			sb.WriteString(fmt.Sprintf("\n--- 文件 %s (%s) ---\n", ec.FileID, ec.FileType))
			if ec.Instruction != "" {
				sb.WriteString(fmt.Sprintf("定向抽取指令: %s\n", ec.Instruction))
			}
			if ec.ExtractedText != "" {
				sb.WriteString(fmt.Sprintf("抽取内容: %s\n", ec.ExtractedText))
			}
			if ec.Summary != "" && ec.Summary != ec.ExtractedText {
				sb.WriteString(fmt.Sprintf("摘要: %s\n", ec.Summary))
			}
		}
	}

	if len(r.StyleGuide.ThemeColors) > 0 || len(r.StyleGuide.Fonts) > 0 || len(r.StyleGuide.Layouts) > 0 {
		sb.WriteString("\n[样式指南]\n")
		if len(r.StyleGuide.ThemeColors) > 0 {
			sb.WriteString(fmt.Sprintf("主色调: %s\n", strings.Join(r.StyleGuide.ThemeColors, ", ")))
		}
		if len(r.StyleGuide.Fonts) > 0 {
			sb.WriteString(fmt.Sprintf("字体: %s\n", strings.Join(r.StyleGuide.Fonts, ", ")))
		}
		if len(r.StyleGuide.Layouts) > 0 {
			sb.WriteString(fmt.Sprintf("版式: %s\n", strings.Join(r.StyleGuide.Layouts, ", ")))
		}
	}

	if len(r.TopicHints) > 0 {
		sb.WriteString("\n[知识补充]\n")
		for _, h := range r.TopicHints {
			h = strings.TrimSpace(h)
			if h != "" {
				sb.WriteString(fmt.Sprintf("- %s\n", h))
			}
		}
	}

	return sb.String()
}

// lookupInstructionByFileID looks up the instruction for a given file ID from the request's reference files.
func lookupInstructionByFileID(refFiles []model.ReferenceFile, fileID string) string {
	for _, f := range refFiles {
		if f.FileID == fileID {
			return f.Instruction
		}
	}
	return ""
}

func findInstruction(fileInstruction, fallback string) string {
	if fileInstruction != "" {
		return fileInstruction
	}
	return fallback
}

func summarizeText(text string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}
	return text[:maxLen] + "..."
}
