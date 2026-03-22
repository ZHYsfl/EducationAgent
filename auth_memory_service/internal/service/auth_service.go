package service

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"auth_memory_service/internal/contract"
	jwtinfra "auth_memory_service/internal/infra/jwt"
	"auth_memory_service/internal/infra/mailer"
	"auth_memory_service/internal/model"
	"auth_memory_service/internal/repository"
	"auth_memory_service/internal/util"
)

type ServiceError struct {
	Code    int
	Message string
}

func (e *ServiceError) Error() string {
	return e.Message
}

type RegisterRequest struct {
	Username    string `json:"username"`
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
	Subject     string `json:"subject"`
	School      string `json:"school"`
}

type RegisterResponse struct {
	UserID                string `json:"user_id"`
	VerificationRequired  bool   `json:"verification_required"`
	VerificationExpiresAt int64  `json:"verification_expires_at"`
}

type VerifyRequest struct {
	Token string `json:"token"`
}

type LoginRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type LoginResponse struct {
	UserID    string `json:"user_id"`
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expires_at"`
}

type ProfileResponse struct {
	UserID      string `json:"user_id"`
	Username    string `json:"username"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	Subject     string `json:"subject"`
	School      string `json:"school"`
	Role        string `json:"role"`
	CreatedAt   int64  `json:"created_at"`
}

type AuthService struct {
	repo                 *repository.AuthRepository
	tokenManager         *jwtinfra.TokenManager
	mailer               mailer.Mailer
	verifyTokenTTLMillis int64
	frontendVerifyURL    string
}

func NewAuthService(repo *repository.AuthRepository, tokenManager *jwtinfra.TokenManager, mailer mailer.Mailer, verifyTokenTTLHours int, frontendVerifyURL string) *AuthService {
	return &AuthService{
		repo:                 repo,
		tokenManager:         tokenManager,
		mailer:               mailer,
		verifyTokenTTLMillis: int64(verifyTokenTTLHours) * 60 * 60 * 1000,
		frontendVerifyURL:    frontendVerifyURL,
	}
}

func (s *AuthService) Register(req RegisterRequest) (RegisterResponse, error) {
	if strings.TrimSpace(req.Username) == "" || strings.TrimSpace(req.Email) == "" || req.Password == "" {
		return RegisterResponse{}, &ServiceError{Code: contract.CodeBadRequest, Message: "missing required field"}
	}
	if len(req.Password) < 8 {
		return RegisterResponse{}, &ServiceError{Code: contract.CodePasswordTooShort, Message: "password too short"}
	}
	username := strings.TrimSpace(req.Username)
	email := strings.TrimSpace(req.Email)

	pendingByUsername, err := s.repo.GetPendingByUsername(username)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return RegisterResponse{}, &ServiceError{Code: contract.CodeInternalError, Message: "internal error"}
	}
	pendingByEmail, err := s.repo.GetPendingByEmail(email)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return RegisterResponse{}, &ServiceError{Code: contract.CodeInternalError, Message: "internal error"}
	}
	if isPendingIdentityRetry(pendingByUsername, pendingByEmail, username, email) {
		return s.refreshPendingAndSend(pendingByUsername, req)
	}
	if pendingByUsername.UserID != "" {
		return RegisterResponse{}, &ServiceError{Code: contract.CodeConflictUsername, Message: "username already exists"}
	}
	if pendingByEmail.UserID != "" {
		return RegisterResponse{}, &ServiceError{Code: contract.CodeConflictEmail, Message: "email already registered"}
	}

	if _, err := s.repo.GetUserByUsername(username); err == nil {
		return RegisterResponse{}, &ServiceError{Code: contract.CodeConflictUsername, Message: "username already exists"}
	} else if !errors.Is(err, repository.ErrNotFound) {
		return RegisterResponse{}, &ServiceError{Code: contract.CodeInternalError, Message: "internal error"}
	}
	if _, err := s.repo.GetUserByEmail(email); err == nil {
		return RegisterResponse{}, &ServiceError{Code: contract.CodeConflictEmail, Message: "email already registered"}
	} else if !errors.Is(err, repository.ErrNotFound) {
		return RegisterResponse{}, &ServiceError{Code: contract.CodeInternalError, Message: "internal error"}
	}

	nowMs := util.NowMilli()
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return RegisterResponse{}, &ServiceError{Code: contract.CodeInternalError, Message: "internal error"}
	}
	token, err := util.NewOpaqueToken()
	if err != nil {
		return RegisterResponse{}, &ServiceError{Code: contract.CodeInternalError, Message: "internal error"}
	}
	tokenHash := util.HashToken(token)
	userID := util.NewUserID()
	pending := model.PendingRegistration{
		UserID:                userID,
		Username:              username,
		Email:                 email,
		PasswordHash:          string(passwordHash),
		DisplayName:           strings.TrimSpace(req.DisplayName),
		Subject:               strings.TrimSpace(req.Subject),
		School:                strings.TrimSpace(req.School),
		Role:                  "teacher",
		VerificationTokenHash: tokenHash,
		VerificationExpiresAt: nowMs + s.verifyTokenTTLMillis,
		VerificationSentAt:    nowMs,
		CreatedAt:             nowMs,
		UpdatedAt:             nowMs,
	}
	if err := s.repo.CreateOrUpdatePending(pending); err != nil {
		return RegisterResponse{}, &ServiceError{Code: contract.CodeInternalError, Message: "internal error"}
	}
	if err := s.repo.RecordIssuedToken(tokenHash, pending.VerificationExpiresAt, nowMs); err != nil {
		return RegisterResponse{}, &ServiceError{Code: contract.CodeInternalError, Message: "internal error"}
	}
	if err := s.mailer.SendVerificationEmail(email, buildVerifyURL(s.frontendVerifyURL, token)); err != nil {
		return RegisterResponse{}, &ServiceError{Code: contract.CodeVerificationEmailSendError, Message: "verification email delivery failed"}
	}
	return RegisterResponse{UserID: userID, VerificationRequired: true, VerificationExpiresAt: pending.VerificationExpiresAt}, nil
}

func (s *AuthService) refreshPendingAndSend(existing model.PendingRegistration, req RegisterRequest) (RegisterResponse, error) {
	nowMs := util.NowMilli()
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return RegisterResponse{}, &ServiceError{Code: contract.CodeInternalError, Message: "internal error"}
	}
	token, err := util.NewOpaqueToken()
	if err != nil {
		return RegisterResponse{}, &ServiceError{Code: contract.CodeInternalError, Message: "internal error"}
	}
	pending := existing
	pending.PasswordHash = string(passwordHash)
	pending.DisplayName = strings.TrimSpace(req.DisplayName)
	pending.Subject = strings.TrimSpace(req.Subject)
	pending.School = strings.TrimSpace(req.School)
	pending.VerificationTokenHash = util.HashToken(token)
	pending.VerificationExpiresAt = nowMs + s.verifyTokenTTLMillis
	pending.VerificationSentAt = nowMs
	pending.UpdatedAt = nowMs
	if err := s.repo.CreateOrUpdatePending(pending); err != nil {
		return RegisterResponse{}, &ServiceError{Code: contract.CodeInternalError, Message: "internal error"}
	}
	if err := s.repo.RecordIssuedToken(pending.VerificationTokenHash, pending.VerificationExpiresAt, nowMs); err != nil {
		return RegisterResponse{}, &ServiceError{Code: contract.CodeInternalError, Message: "internal error"}
	}
	if err := s.mailer.SendVerificationEmail(pending.Email, buildVerifyURL(s.frontendVerifyURL, token)); err != nil {
		return RegisterResponse{}, &ServiceError{Code: contract.CodeVerificationEmailSendError, Message: "verification email delivery failed"}
	}
	return RegisterResponse{UserID: pending.UserID, VerificationRequired: true, VerificationExpiresAt: pending.VerificationExpiresAt}, nil
}

func (s *AuthService) Verify(req VerifyRequest) (LoginResponse, error) {
	if strings.TrimSpace(req.Token) == "" {
		return LoginResponse{}, &ServiceError{Code: contract.CodeBadRequest, Message: "missing token"}
	}
	nowMs := util.NowMilli()
	tokenHash := util.HashToken(strings.TrimSpace(req.Token))
	consumed, err := s.repo.IsVerificationTokenConsumed(tokenHash)
	if err != nil {
		return LoginResponse{}, &ServiceError{Code: contract.CodeInternalError, Message: "internal error"}
	}
	if consumed {
		return LoginResponse{}, &ServiceError{Code: contract.CodeVerificationTokenConsumed, Message: "verification token consumed"}
	}
	pending, err := s.repo.GetPendingByTokenHash(tokenHash)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return LoginResponse{}, &ServiceError{Code: contract.CodeInternalError, Message: "internal error"}
	}
	if errors.Is(err, repository.ErrNotFound) {
		// Deterministic replay/race mapping: if token has been consumed by a prior success,
		// this request must resolve to 40903 even if pending row is already gone.
		consumedAfterNotFound, consumedErr := s.repo.IsVerificationTokenConsumed(tokenHash)
		if consumedErr != nil {
			return LoginResponse{}, &ServiceError{Code: contract.CodeInternalError, Message: "internal error"}
		}
		if consumedAfterNotFound {
			return LoginResponse{}, &ServiceError{Code: contract.CodeVerificationTokenConsumed, Message: "verification token consumed"}
		}
		expiresAt, issuedErr := s.repo.GetIssuedTokenExpiry(tokenHash)
		if issuedErr != nil && !errors.Is(issuedErr, repository.ErrNotFound) {
			return LoginResponse{}, &ServiceError{Code: contract.CodeInternalError, Message: "internal error"}
		}
		if issuedErr == nil && expiresAt < nowMs {
			return LoginResponse{}, &ServiceError{Code: contract.CodeExpiredVerificationToken, Message: "verification token expired"}
		}
		if issuedErr == nil {
			return LoginResponse{}, &ServiceError{Code: contract.CodeVerificationTokenConsumed, Message: "verification token consumed"}
		}
		return LoginResponse{}, &ServiceError{Code: contract.CodeInvalidVerificationToken, Message: "invalid verification token"}
	}
	if pending.VerificationExpiresAt < nowMs {
		return LoginResponse{}, &ServiceError{Code: contract.CodeExpiredVerificationToken, Message: "verification token expired"}
	}
	verifiedUser, err := s.repo.VerifyWithTransaction(tokenHash, nowMs)
	if errors.Is(err, repository.ErrConsumed) {
		return LoginResponse{}, &ServiceError{Code: contract.CodeVerificationTokenConsumed, Message: "verification token consumed"}
	}
	if errors.Is(err, repository.ErrExpired) {
		return LoginResponse{}, &ServiceError{Code: contract.CodeExpiredVerificationToken, Message: "verification token expired"}
	}
	if errors.Is(err, repository.ErrNotFound) {
		// Post-race deterministic consumed re-check for concurrent verify loser.
		consumedAfterTxnNotFound, consumedErr := s.repo.IsVerificationTokenConsumed(tokenHash)
		if consumedErr != nil {
			return LoginResponse{}, &ServiceError{Code: contract.CodeInternalError, Message: "internal error"}
		}
		if consumedAfterTxnNotFound {
			return LoginResponse{}, &ServiceError{Code: contract.CodeVerificationTokenConsumed, Message: "verification token consumed"}
		}
		expiresAt, issuedErr := s.repo.GetIssuedTokenExpiry(tokenHash)
		if issuedErr != nil && !errors.Is(issuedErr, repository.ErrNotFound) {
			return LoginResponse{}, &ServiceError{Code: contract.CodeInternalError, Message: "internal error"}
		}
		if issuedErr == nil && expiresAt < nowMs {
			return LoginResponse{}, &ServiceError{Code: contract.CodeExpiredVerificationToken, Message: "verification token expired"}
		}
		if issuedErr == nil {
			return LoginResponse{}, &ServiceError{Code: contract.CodeVerificationTokenConsumed, Message: "verification token consumed"}
		}
		return LoginResponse{}, &ServiceError{Code: contract.CodeInvalidVerificationToken, Message: "invalid verification token"}
	}
	if err != nil {
		return LoginResponse{}, &ServiceError{Code: contract.CodeInternalError, Message: "internal error"}
	}
	token, expiresAt, err := s.tokenManager.Generate(verifiedUser.ID, verifiedUser.Username)
	if err != nil {
		return LoginResponse{}, &ServiceError{Code: contract.CodeInternalError, Message: "internal error"}
	}
	return LoginResponse{UserID: verifiedUser.ID, Token: token, ExpiresAt: expiresAt}, nil
}

func (s *AuthService) Login(req LoginRequest) (LoginResponse, error) {
	if strings.TrimSpace(req.Password) == "" {
		return LoginResponse{}, &ServiceError{Code: contract.CodeBadRequest, Message: "missing required field"}
	}
	if strings.TrimSpace(req.Username) == "" && strings.TrimSpace(req.Email) == "" {
		return LoginResponse{}, &ServiceError{Code: contract.CodeBadRequest, Message: "missing required field"}
	}
	var user model.User
	var err error
	if strings.TrimSpace(req.Username) != "" {
		user, err = s.repo.GetUserByUsername(strings.TrimSpace(req.Username))
	} else {
		user, err = s.repo.GetUserByEmail(strings.TrimSpace(req.Email))
	}
	if errors.Is(err, repository.ErrNotFound) {
		if s.hasMatchingPending(req.Username, req.Email) {
			return LoginResponse{}, &ServiceError{Code: contract.CodeEmailNotVerified, Message: "email not verified"}
		}
		return LoginResponse{}, &ServiceError{Code: contract.CodeInvalidCredentials, Message: "invalid credentials"}
	}
	if err != nil {
		return LoginResponse{}, &ServiceError{Code: contract.CodeInternalError, Message: "internal error"}
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)) != nil {
		return LoginResponse{}, &ServiceError{Code: contract.CodeInvalidCredentials, Message: "invalid credentials"}
	}
	token, expiresAt, err := s.tokenManager.Generate(user.ID, user.Username)
	if err != nil {
		return LoginResponse{}, &ServiceError{Code: contract.CodeInternalError, Message: "internal error"}
	}
	return LoginResponse{UserID: user.ID, Token: token, ExpiresAt: expiresAt}, nil
}

func (s *AuthService) GetProfile(userID string) (ProfileResponse, error) {
	if strings.TrimSpace(userID) == "" {
		return ProfileResponse{}, &ServiceError{Code: contract.CodeBadRequest, Message: "missing required field"}
	}
	user, err := s.repo.GetUserByID(userID)
	if errors.Is(err, repository.ErrNotFound) {
		return ProfileResponse{}, &ServiceError{Code: contract.CodeInvalidCredentials, Message: "invalid credentials"}
	}
	if err != nil {
		return ProfileResponse{}, &ServiceError{Code: contract.CodeInternalError, Message: "internal error"}
	}
	return ProfileResponse{
		UserID:      user.ID,
		Username:    user.Username,
		Email:       user.Email,
		DisplayName: user.DisplayName,
		Subject:     user.Subject,
		School:      user.School,
		Role:        user.Role,
		CreatedAt:   user.CreatedAt,
	}, nil
}

func (s *AuthService) hasMatchingPending(username, email string) bool {
	if strings.TrimSpace(username) != "" {
		if _, err := s.repo.GetPendingByUsername(strings.TrimSpace(username)); err == nil {
			return true
		}
	}
	if strings.TrimSpace(email) != "" {
		if _, err := s.repo.GetPendingByEmail(strings.TrimSpace(email)); err == nil {
			return true
		}
	}
	return false
}

func isPendingIdentityRetry(pendingByUsername, pendingByEmail model.PendingRegistration, username, email string) bool {
	if pendingByUsername.UserID == "" || pendingByEmail.UserID == "" {
		return false
	}
	if pendingByUsername.UserID != pendingByEmail.UserID {
		return false
	}
	return pendingByUsername.Username == username && pendingByUsername.Email == email
}

func buildVerifyURL(baseURL, token string) string {
	u, err := url.Parse(baseURL)
	if err != nil {
		return fmt.Sprintf("%s?token=%s", baseURL, url.QueryEscape(token))
	}
	q := u.Query()
	q.Set("token", token)
	u.RawQuery = q.Encode()
	return u.String()
}
