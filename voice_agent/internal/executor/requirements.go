package executor

import (
	"context"
	"encoding/json"
)

func (e *Executor) executeUpdateRequirements(_ context.Context, params map[string]string, _ SessionContext) string {
	if len(params) == 0 {
		return "Error: no fields to update"
	}

	// 返回 JSON 格式，Pipeline 通过 msgType="requirements_updated" 识别
	data, _ := json.Marshal(params)
	return string(data)
}
