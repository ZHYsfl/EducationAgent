package handler

import (
	"encoding/json"
	"net/http"

	"educationagent/internal/state"

	"github.com/gin-gonic/gin"
)

// ArmLogStream handles GET /api/v1/arm/log-stream (SSE): live activity log of
// the background arm agent (tool calls, tool results, assistant text, errors).
func ArmLogStream(st *state.AppState) gin.HandlerFunc {
	return func(c *gin.Context) {
		ch, replay := st.SubscribeArmLog()
		defer st.UnsubscribeArmLog(ch)

		c.Writer.Header().Set("Content-Type", "text/event-stream")
		c.Writer.Header().Set("Cache-Control", "no-cache")
		c.Writer.Header().Set("Connection", "keep-alive")
		c.Writer.WriteHeader(http.StatusOK)

		for _, msg := range replay {
			data, _ := json.Marshal(msg)
			c.Writer.Write([]byte("data: "))
			c.Writer.Write(data)
			c.Writer.Write([]byte("\n\n"))
			c.Writer.Flush()
		}

		for {
			select {
			case msg, ok := <-ch:
				if !ok {
					return
				}
				data, _ := json.Marshal(msg)
				c.Writer.Write([]byte("data: "))
				c.Writer.Write(data)
				c.Writer.Write([]byte("\n\n"))
				c.Writer.Flush()
			case <-c.Request.Context().Done():
				return
			}
		}
	}
}
