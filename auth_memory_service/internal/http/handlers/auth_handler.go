package handlers

import (
	"github.com/gin-gonic/gin"

	"auth_memory_service/internal/contract"
	"auth_memory_service/internal/service"
)

type AuthHandler struct {
	authService *service.AuthService
}

func NewAuthHandler(authService *service.AuthService) *AuthHandler {
	return &AuthHandler{authService: authService}
}

func (h *AuthHandler) Register(c *gin.Context) {
	var req service.RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		contract.Error(c, contract.CodeBadRequest, "missing required field")
		return
	}
	resp, err := h.authService.Register(req)
	if err != nil {
		h.handleError(c, err)
		return
	}
	contract.Success(c, resp, "verification email sent")
}

func (h *AuthHandler) Verify(c *gin.Context) {
	var req service.VerifyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		contract.Error(c, contract.CodeBadRequest, "missing token")
		return
	}
	resp, err := h.authService.Verify(req)
	if err != nil {
		h.handleError(c, err)
		return
	}
	contract.Success(c, resp, "verified")
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req service.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		contract.Error(c, contract.CodeBadRequest, "missing required field")
		return
	}
	resp, err := h.authService.Login(req)
	if err != nil {
		h.handleError(c, err)
		return
	}
	contract.Success(c, resp, "")
}

func (h *AuthHandler) Profile(c *gin.Context) {
	userID := c.GetString("user_id")
	resp, err := h.authService.GetProfile(userID)
	if err != nil {
		h.handleError(c, err)
		return
	}
	contract.Success(c, resp, "")
}

func (h *AuthHandler) handleError(c *gin.Context, err error) {
	se, ok := err.(*service.ServiceError)
	if !ok {
		contract.Error(c, contract.CodeInternalError, "internal error")
		return
	}
	contract.Error(c, se.Code, se.Message)
}
