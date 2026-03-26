package executor

import (
	"context"
	"fmt"
	"log"

	"voiceagent/internal/types"
)

func (e *Executor) executePPTInit(ctx context.Context, params map[string]string) string {
	if e.clients == nil {
		return "Error: PPT service not available"
	}

	req := types.PPTInitRequest{
		Topic:       params["topic"],
		Description: params["desc"],
	}

	resp, err := e.clients.InitPPT(ctx, req)
	if err != nil {
		log.Printf("[executor] ppt_init error: %v", err)
		return fmt.Sprintf("PPT初始化失败: %v", err)
	}

	return fmt.Sprintf("PPT任务已创建，TaskID: %s", resp.TaskID)
}

func (e *Executor) executePPTModify(ctx context.Context, params map[string]string) string {
	if e.clients == nil {
		return "Error: PPT service not available"
	}

	req := types.PPTFeedbackRequest{
		TaskID:        params["task"],
		ViewingPageID: params["page"],
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
