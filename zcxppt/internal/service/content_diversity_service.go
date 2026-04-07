package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	openai "github.com/openai/openai-go/v3"
	toolcalling "tool_calling_go"

	"zcxppt/internal/infra/oss"
	"zcxppt/internal/model"
)

var ErrInvalidContentDiversityRequest = errors.New("invalid content diversity request")

// ContentDiversityService generates 动画创意 and 互动小游戏.
type ContentDiversityService struct {
	resultRepo    map[string]*contentDiversityJob
	resultRepoMu  sync.RWMutex
	httpClient    *http.Client
	llmCfg        LLMClientConfig
	renderConfig  RenderServiceConfig
	ossClient     *oss.Client
}

type contentDiversityJob struct {
	ResultID   string
	TaskID     string
	Status     string
	Animations []model.AnimationResult
	Games      []model.GameResult
	Error      string
	CreatedAt  time.Time
}

// NewContentDiversityService creates a new ContentDiversityService.
func NewContentDiversityService(llmCfg LLMClientConfig, renderCfg RenderServiceConfig, ossClient *oss.Client) *ContentDiversityService {
	return &ContentDiversityService{
		resultRepo:   make(map[string]*contentDiversityJob),
		httpClient:   &http.Client{Timeout: 30 * time.Second},
		llmCfg:       llmCfg,
		renderConfig: renderCfg,
		ossClient:    ossClient,
	}
}

// Generate starts async content diversity generation (animation + games).
func (s *ContentDiversityService) Generate(ctx context.Context, req model.ContentDiversityRequest) (model.ContentDiversityResponse, error) {
	if strings.TrimSpace(req.Topic) == "" {
		return model.ContentDiversityResponse{}, ErrInvalidContentDiversityRequest
	}

	resultID := "div_" + uuid.NewString()
	job := &contentDiversityJob{
		ResultID:  resultID,
		TaskID:    req.TaskID,
		Status:    "generating",
		CreatedAt: time.Now(),
	}

	s.resultRepoMu.Lock()
	s.resultRepo[resultID] = job
	s.resultRepoMu.Unlock()

	go s.runGeneration(resultID, req)

	return model.ContentDiversityResponse{
		TaskID:   req.TaskID,
		ResultID: resultID,
		Status:   "generating",
	}, nil
}

func (s *ContentDiversityService) runGeneration(resultID string, req model.ContentDiversityRequest) {
	defer func() {
		if r := recover(); r != nil {
			s.resultRepoMu.Lock()
			if job, ok := s.resultRepo[resultID]; ok {
				job.Status = "failed"
				job.Error = fmt.Sprintf("panic: %v", r)
			}
			s.resultRepoMu.Unlock()
		}
	}()

	ctx := context.Background()
	contentType := strings.ToLower(strings.TrimSpace(req.Type))
	if contentType == "" {
		contentType = "both"
	}

	var animations []model.AnimationResult
	var games []model.GameResult
	var errs []string

	if contentType == "animation" || contentType == "both" {
		anims, err := s.generateAnimations(ctx, req)
		if err != nil {
			errs = append(errs, fmt.Sprintf("animation: %v", err))
		} else {
			animations = anims
		}
	}

	if contentType == "game" || contentType == "both" {
		gm, err := s.generateGames(ctx, req)
		if err != nil {
			errs = append(errs, fmt.Sprintf("game: %v", err))
		} else {
			games = gm
		}
	}

	status := "completed"
	var errMsg string
	if len(animations) == 0 && len(games) == 0 && len(errs) > 0 {
		status = "failed"
		errMsg = strings.Join(errs, "; ")
	}

	s.resultRepoMu.Lock()
	if job, ok := s.resultRepo[resultID]; ok {
		job.Status = status
		job.Animations = animations
		job.Games = games
		if errMsg != "" {
			job.Error = errMsg
		}
	}
	s.resultRepoMu.Unlock()
}

// generateAnimations generates 动画创意 HTML5 content using LLM.
func (s *ContentDiversityService) generateAnimations(ctx context.Context, req model.ContentDiversityRequest) ([]model.AnimationResult, error) {
	if strings.TrimSpace(s.llmCfg.APIKey) == "" || strings.TrimSpace(s.llmCfg.Model) == "" {
		return nil, errors.New("llm config not complete")
	}

	style := strings.TrimSpace(req.AnimationStyle)
	if style == "" {
		style = "all"
	}

	system := `你是一个教学动画创意设计师。请根据指定的知识点，生成多个动画创意描述和对应的HTML5/CSS3动画代码。

严格输出 JSON 对象数组：
[
  {
    "animation_id": "anim_1",
    "title": "动画标题",
    "description": "动画创意描述，说明动画如何展示知识点",
    "html_content": "完整的HTML5/CSS3/JS动画代码（inline），包含<style>和<script>标签，可直接在浏览器中运行"
  },
  ...
]

要求：
1. 生成2-3个不同风格的动画创意
2. 每个动画使用纯HTML5/CSS3/JS，不需要外部依赖
3. 动画内容与知识点(topic)紧密相关
4. 每个动画设计时长3-8秒，适合课堂教学展示
5. 支持的动画风格包括：slide_in(滑入)、fade(淡入淡出)、zoom(缩放)、draw(绘制)、pulse(脉冲)
6. 动画背景色使用浅色(#f0f4f8)，主色调#1F4E79
7. 动画代码必须完整可运行，包含<!DOCTYPE html>、<html>、<head>、<body>标签
8. 动画应在页面加载后自动播放，循环播放
9. 所有文字使用中文
10. 不要输出JSON之外的任何文字`

	pageContext := ""
	if req.PageCode != "" {
		pageContext = fmt.Sprintf("\n相关PPT页面代码:\n%s\n", req.PageCode)
	}

	prompt := fmt.Sprintf("topic=%s\nsubject=%s\nanimation_style=%s\nkb_summary=%s%s",
		req.Topic,
		req.Subject,
		style,
		req.KBSummary,
		pageContext,
	)

	agent := toolcalling.NewAgent(toolcalling.LLMConfig{
		APIKey:  s.llmCfg.APIKey,
		BaseURL: s.llmCfg.BaseURL,
		Model:   s.llmCfg.Model,
	})

	msgs := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(system),
		openai.UserMessage(prompt),
	}

	resp, err := agent.Chat(ctx, msgs)
	if err != nil {
		return nil, err
	}

	lastText := ""
	for i := len(resp) - 1; i >= 0; i-- {
		if resp[i].OfAssistant != nil && resp[i].OfAssistant.Content.OfString.Valid() {
			lastText = resp[i].OfAssistant.Content.OfString.Value
			break
		}
	}
	if lastText == "" {
		return nil, errors.New("llm returned no animation response")
	}

	cleaned := strings.TrimSpace(lastText)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	cleaned = strings.TrimSpace(cleaned)

	var rawAnims []struct {
		AnimationID string `json:"animation_id"`
		Title      string `json:"title"`
		Description string `json:"description"`
		HTMLContent string `json:"html_content"`
	}
	if err := json.Unmarshal([]byte(cleaned), &rawAnims); err != nil {
		return nil, fmt.Errorf("parse animation json: %w", err)
	}

	var results []model.AnimationResult
	for _, a := range rawAnims {
		animID := a.AnimationID
		if animID == "" {
			animID = "anim_" + uuid.NewString()[:8]
		}

		htmlURL := ""
		if a.HTMLContent != "" && s.ossClient != nil {
			htmlURL = s.uploadHTMLContent(ctx, animID, a.HTMLContent)
		}

		results = append(results, model.AnimationResult{
			AnimationID:   animID,
			Title:        a.Title,
			Description:  a.Description,
			HTMLContent:  a.HTMLContent,
			HTMLURL:      htmlURL,
			ExportFormats: []string{"html5"},
		})
	}

	return results, nil
}

// generateGames generates 互动小游戏 HTML5 content using LLM.
func (s *ContentDiversityService) generateGames(ctx context.Context, req model.ContentDiversityRequest) ([]model.GameResult, error) {
	if strings.TrimSpace(s.llmCfg.APIKey) == "" || strings.TrimSpace(s.llmCfg.Model) == "" {
		return nil, errors.New("llm config not complete")
	}

	gameType := strings.TrimSpace(req.GameType)
	if gameType == "" || gameType == "random" {
		gameType = "quiz"
	}

	system := fmt.Sprintf(`你是一个课堂教学互动游戏设计师。请根据指定的知识点，生成一个互动小游戏的完整HTML5代码。

严格输出 JSON：
{
  "game_id": "game_1",
  "title": "游戏标题",
  "game_type": "%s",
  "html_content": "完整的HTML5/CSS3/JS游戏代码，可直接在浏览器中运行"
}

游戏类型说明：
- quiz: 选择题游戏（4选1），包含题目、选项、答案验证、得分显示
- matching: 连连看匹配游戏，将左侧词汇与右侧定义配对
- ordering: 排序题，将选项按正确顺序排列
- fill_blank: 填空题游戏

要求：
1. 游戏代码必须完整可运行，包含<!DOCTYPE html>、<html>、<head>、<body>标签
2. 使用纯HTML5/CSS3/JS，不需要外部依赖
3. 游戏与知识点(topic)紧密相关
4. 游戏包含开始按钮、题目展示、交互反馈、结果展示
5. 交互反馈：答对显示绿色+鼓励语，答错显示红色+正确答案
6. 界面风格：标题栏#1F4E79背景，游戏区白色背景，答案选项#4472C4
7. 所有文字使用中文
8. 游戏应该有适当的动画效果（淡入淡出、选项高亮）
9. 不要输出JSON之外的任何文字`, gameType)

	pageContext := ""
	if req.PageCode != "" {
		pageContext = fmt.Sprintf("\n相关PPT页面代码:\n%s\n", req.PageCode)
	}

	prompt := fmt.Sprintf("topic=%s\nsubject=%s\ngame_type=%s\nkb_summary=%s%s",
		req.Topic,
		req.Subject,
		gameType,
		req.KBSummary,
		pageContext,
	)

	agent := toolcalling.NewAgent(toolcalling.LLMConfig{
		APIKey:  s.llmCfg.APIKey,
		BaseURL: s.llmCfg.BaseURL,
		Model:   s.llmCfg.Model,
	})

	msgs := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(system),
		openai.UserMessage(prompt),
	}

	resp, err := agent.Chat(ctx, msgs)
	if err != nil {
		return nil, err
	}

	lastText := ""
	for i := len(resp) - 1; i >= 0; i-- {
		if resp[i].OfAssistant != nil && resp[i].OfAssistant.Content.OfString.Valid() {
			lastText = resp[i].OfAssistant.Content.OfString.Value
			break
		}
	}
	if lastText == "" {
		return nil, errors.New("llm returned no game response")
	}

	cleaned := strings.TrimSpace(lastText)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	cleaned = strings.TrimSpace(cleaned)

	var rawGame struct {
		GameID     string `json:"game_id"`
		Title      string `json:"title"`
		GameType   string `json:"game_type"`
		HTMLContent string `json:"html_content"`
	}
	if err := json.Unmarshal([]byte(cleaned), &rawGame); err != nil {
		return nil, fmt.Errorf("parse game json: %w", err)
	}

	gameID := rawGame.GameID
	if gameID == "" {
		gameID = "game_" + uuid.NewString()[:8]
	}
	gameTypeStr := rawGame.GameType
	if gameTypeStr == "" {
		gameTypeStr = gameType
	}

	htmlURL := ""
	if rawGame.HTMLContent != "" && s.ossClient != nil {
		htmlURL = s.uploadHTMLContent(ctx, gameID, rawGame.HTMLContent)
	}

	return []model.GameResult{
		{
			GameID:       gameID,
			Title:       rawGame.Title,
			GameType:    gameTypeStr,
			HTMLContent: rawGame.HTMLContent,
			HTMLURL:     htmlURL,
			ExportFormats: []string{"html5"},
		},
	}, nil
}

// uploadHTMLContent saves HTML content to OSS and returns the URL.
func (s *ContentDiversityService) uploadHTMLContent(ctx context.Context, contentID, htmlContent string) string {
	if s.ossClient == nil {
		return ""
	}
	data := []byte(htmlContent)
	objectKey := fmt.Sprintf("content_diversity/%s/%s.html", contentID, contentID)
	url, _, err := s.ossClient.PutObject(ctx, objectKey, data)
	if err != nil {
		return ""
	}
	return url
}

// ExportAnimation exports animation content in the specified format (html5/gif/mp4).
func (s *ContentDiversityService) ExportAnimation(ctx context.Context, resultID, animationID, format string) (model.ExportContentResponse, error) {
	s.resultRepoMu.RLock()
	job, ok := s.resultRepo[resultID]
	s.resultRepoMu.RUnlock()

	if !ok {
		return model.ExportContentResponse{}, errors.New("result not found")
	}

	var targetAnim *model.AnimationResult
	for i := range job.Animations {
		if job.Animations[i].AnimationID == animationID {
			targetAnim = &job.Animations[i]
			break
		}
	}
	if targetAnim == nil {
		return model.ExportContentResponse{}, errors.New("animation not found")
	}

	format = strings.ToLower(strings.TrimSpace(format))
	if format == "" {
		format = "html5"
	}

	downloadURL := targetAnim.HTMLURL

	if format == "gif" || format == "mp4" {
		url, err := s.exportToGIFMP4(ctx, animationID, targetAnim.HTMLContent, format)
		if err != nil {
			return model.ExportContentResponse{ResultID: animationID, Format: format, Status: "failed", Error: err.Error()}, nil
		}
		downloadURL = url
	}

	return model.ExportContentResponse{
		ResultID:    animationID,
		Format:      format,
		Status:      "completed",
		DownloadURL: downloadURL,
	}, nil
}

// exportToGIFMP4 converts HTML animation to GIF or MP4 using render_animation.py.
func (s *ContentDiversityService) exportToGIFMP4(ctx context.Context, contentID, htmlContent, format string) (string, error) {
	tmpDir := s.renderConfig.RenderDir
	if tmpDir == "" {
		tmpDir = os.TempDir()
	}
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return "", fmt.Errorf("create dir: %w", err)
	}

	htmlPath := filepath.Join(tmpDir, fmt.Sprintf("%s_%s.html", contentID, format))
	if err := os.WriteFile(htmlPath, []byte(htmlContent), 0644); err != nil {
		return "", fmt.Errorf("write html: %w", err)
	}
	defer os.Remove(htmlPath)

	var outputExt string
	if format == "gif" {
		outputExt = "gif"
	} else {
		outputExt = "mp4"
	}
	outputPath := filepath.Join(tmpDir, fmt.Sprintf("%s_%s.%s", contentID, format, outputExt))

	// Resolve render_animation.py alongside the configured script path
	scriptPath := s.renderConfig.ScriptPath
	if scriptPath == "" {
		return "", errors.New("render script path not configured")
	}
	animScriptPath := filepath.Join(filepath.Dir(scriptPath), "render_animation.py")
	if _, err := os.Stat(animScriptPath); os.IsNotExist(err) {
		return "", fmt.Errorf("render_animation.py not found at %s", animScriptPath)
	}

	// Build stdin JSON matching render_animation.py's expected format
	inputJSON := map[string]any{
		"output_path": outputPath,
		"format":      format,
		"title":       contentID,
		"data": map[string]any{
			"html_content": htmlContent,
		},
	}
	inputBytes, _ := json.Marshal(inputJSON)

	cmdCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	pythonPath := s.renderConfig.PythonPath
	if pythonPath == "" {
		pythonPath = "python"
	}

	cmd := exec.CommandContext(cmdCtx, pythonPath, animScriptPath)
	cmd.Stdin = bytes.NewReader(inputBytes)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	_ = cmd.Run()

	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		return "", fmt.Errorf("export failed: %s", stderr.String())
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		return "", fmt.Errorf("read output: %w", err)
	}

	ext := filepath.Ext(outputPath)
	objectKey := fmt.Sprintf("content_diversity/%s/%s_%s%s", contentID, contentID, format, ext)
	url, _, err := s.ossClient.PutObject(ctx, objectKey, data)
	if err != nil {
		return "", fmt.Errorf("upload: %w", err)
	}

	return url, nil
}

// ExportGame exports game content (currently only html5 supported).
func (s *ContentDiversityService) ExportGame(ctx context.Context, resultID, gameID, format string) (model.ExportContentResponse, error) {
	s.resultRepoMu.RLock()
	job, ok := s.resultRepo[resultID]
	s.resultRepoMu.RUnlock()

	if !ok {
		return model.ExportContentResponse{}, errors.New("result not found")
	}

	var targetGame *model.GameResult
	for i := range job.Games {
		if job.Games[i].GameID == gameID {
			targetGame = &job.Games[i]
			break
		}
	}
	if targetGame == nil {
		return model.ExportContentResponse{}, errors.New("game not found")
	}

	format = strings.ToLower(strings.TrimSpace(format))
	if format == "" {
		format = "html5"
	}

	downloadURL := targetGame.HTMLURL
	if format == "gif" {
		url, err := s.exportGameToGIF(ctx, gameID, targetGame.HTMLContent)
		if err != nil {
			return model.ExportContentResponse{ResultID: gameID, Format: format, Status: "failed", Error: err.Error()}, nil
		}
		downloadURL = url
	}

	return model.ExportContentResponse{
		ResultID:    gameID,
		Format:      format,
		Status:      "completed",
		DownloadURL: downloadURL,
	}, nil
}

func (s *ContentDiversityService) exportGameToGIF(ctx context.Context, gameID, htmlContent string) (string, error) {
	return s.exportToGIFMP4(ctx, gameID, htmlContent, "gif")
}

// Integrate embeds animation/game content into a PPT page's py_code.
func (s *ContentDiversityService) Integrate(ctx context.Context, req model.IntegrationRequest) (model.IntegrationResponse, error) {
	pyCode := req.PageID
	if req.AnimationIDs == nil && req.GameIDs == nil {
		return model.IntegrationResponse{
			TaskID: req.TaskID,
			PageID: req.PageID,
			Status: "failed",
			Error:  "no animation_ids or game_ids provided",
		}, nil
	}

	s.resultRepoMu.RLock()
	var job *contentDiversityJob
	for _, j := range s.resultRepo {
		if j.TaskID == req.TaskID {
			job = j
			break
		}
	}
	s.resultRepoMu.RUnlock()

	if job == nil {
		return model.IntegrationResponse{
			TaskID: req.TaskID,
			PageID: req.PageID,
			Status: "failed",
			Error:  "content diversity result not found for this task",
		}, nil
	}

	var descriptions []string
	for _, aid := range req.AnimationIDs {
		for _, a := range job.Animations {
			if a.AnimationID == aid {
				descriptions = append(descriptions, fmt.Sprintf("【动画】%s: %s", a.Title, a.Description))
			}
		}
	}
	for _, gid := range req.GameIDs {
		for _, g := range job.Games {
			if g.GameID == gid {
				descriptions = append(descriptions, fmt.Sprintf("【互动游戏】%s: %s", g.Title, g.GameType))
			}
		}
	}

	position := strings.TrimSpace(req.Position)
	if position == "" {
		position = "footer"
	}

	integrationNote := fmt.Sprintf("\n# === 嵌入内容 (%s) ===\n", position)
	for _, d := range descriptions {
		integrationNote += fmt.Sprintf("# %s\n", d)
	}
	integrationNote += "# 注意：请在PPT备注区域或附页中展示对应HTML5内容\n"

	return model.IntegrationResponse{
		TaskID:       req.TaskID,
		PageID:       req.PageID,
		Status:       "completed",
		UpdatedPyCode: pyCode + integrationNote,
	}, nil
}

// GetStatus returns the current generation status.
func (s *ContentDiversityService) GetStatus(resultID string) (model.ContentDiversityResponse, error) {
	s.resultRepoMu.RLock()
	job, ok := s.resultRepo[resultID]
	s.resultRepoMu.RUnlock()

	if !ok {
		return model.ContentDiversityResponse{}, errors.New("result not found")
	}

	return model.ContentDiversityResponse{
		TaskID:     job.TaskID,
		ResultID:   resultID,
		Status:     job.Status,
		Animations: job.Animations,
		Games:      job.Games,
		Error:     job.Error,
	}, nil
}
