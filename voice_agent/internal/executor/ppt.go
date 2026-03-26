package executor

import (
	"context"
	"fmt"
	"log"
	"strings"

	"voiceagent/internal/types"
)

func (e *Executor) executePPTInit(ctx context.Context, params map[string]string, sessionCtx SessionContext) string {
	if e.clients == nil {
		return "Error: PPT service not available"
	}

	// 检查必填字段
	missing := checkRequiredFields(sessionCtx)
	if len(missing) > 0 {
		return fmt.Sprintf("Error: 缺少必填信息，请先提供: %s", strings.Join(missing, ", "))
	}

	teachingElements := &types.InitTeachingElements{
		KnowledgePoints:   sessionCtx.KnowledgePoints,
		TeachingGoals:     sessionCtx.TeachingGoals,
		TeachingLogic:     sessionCtx.TeachingLogic,
		KeyDifficulties:   sessionCtx.KeyDifficulties,
		Duration:          sessionCtx.Duration,
		InteractionDesign: sessionCtx.InteractionDesign,
		OutputFormats:     sessionCtx.OutputFormats,
	}

	var refFiles []types.ReferenceFile
	for _, rf := range sessionCtx.ReferenceFiles {
		refFiles = append(refFiles, types.ReferenceFile{
			FileID:      rf.FileID,
			FileURL:     rf.FileURL,
			FileType:    rf.FileType,
			Instruction: rf.Instruction,
		})
	}

	req := types.PPTInitRequest{
		UserID:           sessionCtx.UserID,
		SessionID:        sessionCtx.SessionID,
		Topic:            sessionCtx.Topic,
		Description:      params["desc"],
		TotalPages:       sessionCtx.TotalPages,
		Audience:         sessionCtx.Audience,
		GlobalStyle:      sessionCtx.GlobalStyle,
		TeachingElements: teachingElements,
		ReferenceFiles:   refFiles,
	}

	resp, err := e.clients.InitPPT(ctx, req)
	if err != nil {
		log.Printf("[executor] ppt_init error: %v", err)
		return fmt.Sprintf("PPT初始化失败: %v", err)
	}

	return fmt.Sprintf("PPT任务已创建，TaskID: %s", resp.TaskID)
}

func (e *Executor) executePPTModify(ctx context.Context, params map[string]string, sessionCtx SessionContext) string {
	if e.clients == nil {
		return "Error: PPT service not available"
	}

	req := types.PPTFeedbackRequest{
		TaskID:        sessionCtx.ActiveTaskID,
		BaseTimestamp: sessionCtx.BaseTimestamp,
		ViewingPageID: sessionCtx.ViewingPageID,
		RawText:       params["raw_text"],
		Intents:       nil, // PPT Agent 负责解析
	}

	err := e.clients.SendFeedback(ctx, req)
	if err != nil {
		log.Printf("[executor] ppt_mod error: %v", err)
		return fmt.Sprintf("PPT修改失败: %v", err)
	}

	return "PPT修改请求已发送"
}

func checkRequiredFields(ctx SessionContext) []string {
	var missing []string
	if ctx.Topic == "" {
		missing = append(missing, "topic")
	}
	if ctx.Audience == "" {
		missing = append(missing, "target_audience")
	}
	if len(ctx.KnowledgePoints) == 0 {
		missing = append(missing, "knowledge_points")
	}
	if len(ctx.TeachingGoals) == 0 {
		missing = append(missing, "teaching_goals")
	}
	if ctx.TeachingLogic == "" {
		missing = append(missing, "teaching_logic")
	}
	if len(ctx.KeyDifficulties) == 0 {
		missing = append(missing, "key_difficulties")
	}
	if ctx.Duration == "" {
		missing = append(missing, "duration")
	}
	if ctx.TotalPages == 0 {
		missing = append(missing, "total_pages")
	}
	if ctx.GlobalStyle == "" {
		missing = append(missing, "global_style")
	}
	if ctx.InteractionDesign == "" {
		missing = append(missing, "interaction_design")
	}
	if len(ctx.OutputFormats) == 0 {
		missing = append(missing, "output_formats")
	}
	return missing
}
