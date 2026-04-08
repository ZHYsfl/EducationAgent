package executor

import (
	"context"
	"encoding/json"
)

func (e *Executor) executeUpdateRequirements(_ context.Context, params map[string]string, _ SessionContext) string {
	if len(params) == 0 {
		return "Error: no fields to update"
	}

	// 返回 JSON 格式，Pipeline 通过 event_type="update_requirements" 识别
	data, _ := json.Marshal(params)
	return string(data)
}

func (e *Executor) executeRequireConfirm(_ context.Context, _ map[string]string, _ SessionContext) string {
	return "已发送确认请求，等待用户确认"
}
