package handler

import (
	"strings"

	"educationagent/internal/middleware"
	"educationagent/internal/model"
	"educationagent/internal/service"
	"educationagent/internal/state"

	"github.com/gin-gonic/gin"
)

// VoiceSendToArmAgent handles POST /api/v1/send_to_arm_agent — the voice agent
// (or a tester) forwards a task/change/cancel message to the arm agent.
func VoiceSendToArmAgent(armSvc *service.ArmService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req model.SendToArmAgentRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			middleware.Fail(c, 400, "invalid request body")
			return
		}

		if err := armSvc.OnVoiceMessage(req.Content); err != nil {
			middleware.Fail(c, 400, "failed to send the message to the arm agent")
			return
		}
		middleware.OK(c, "发送成功")
	}
}

// VoiceGetMessageFromArmAgent handles POST /api/v1/get_message_from_arm_agent —
// drains message_from_arm_agent_queue and returns all pending arm messages
// joined with ";", per the tool contract in api_of_voice_tools.md §2.2.
func VoiceGetMessageFromArmAgent(st *state.AppState) gin.HandlerFunc {
	return func(c *gin.Context) {
		msgs := st.DrainArmMessageQueue()
		if len(msgs) == 0 {
			middleware.OK(c, "当前没有新消息")
			return
		}
		middleware.OK(c, "all_messages_from_arm_agent:"+strings.Join(msgs, ";"))
	}
}

// ArmSendToVoiceAgent handles POST /api/v1/send_to_voice_agent — enqueues an
// arm-side message into message_from_arm_agent_queue (test/联调 endpoint; the
// arm agent itself uses its send_to_voice_agent tool).
func ArmSendToVoiceAgent(armSvc *service.ArmService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req model.SendToVoiceAgentRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			middleware.Fail(c, 400, "invalid request body")
			return
		}

		if err := armSvc.SendToVoiceAgent(req.Content); err != nil {
			middleware.Fail(c, 400, "failed to send the message to the voice agent")
			return
		}
		middleware.OK(c, "发送成功")
	}
}

// StartConversation handles POST /api/v1/start_conversation
func StartConversation(st *state.AppState, armSvc *service.ArmService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req model.StartConversationRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			middleware.Fail(c, 400, "invalid request body")
			return
		}

		armSvc.StopRuntime()
		st.LockVoiceTurn()
		st.ResetConversation()
		st.UnlockVoiceTurn()
		middleware.OK(c, nil)
	}
}
