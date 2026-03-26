package executor

import (
	"context"
	"fmt"
	"log"

	"voiceagent/internal/types"
)

func (e *Executor) executePPTInit(ctx context.Context, params map[string]string, sessionCtx SessionContext) string {
	if e.clients == nil {
		return "Error: PPT service not available"
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
		Topic:            params["topic"],
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
		RawText:       params["ins"],
		Intents: []types.Intent{{
			ActionType:   params["action"],
			TargetPageID: params["page"],
			Instruction:  params["ins"],
		}},
	}

	err := e.clients.SendFeedback(ctx, req)
	if err != nil {
		log.Printf("[executor] ppt_mod error: %v", err)
		return fmt.Sprintf("PPT修改失败: %v", err)
	}

	return "PPT修改请求已发送"
}
