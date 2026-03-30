package service

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"auth_memory_service/internal/contract"
	"auth_memory_service/internal/infra/extractor"
	"auth_memory_service/internal/model"
	"auth_memory_service/internal/repository"
	"auth_memory_service/internal/util"
)

type MemoryService struct {
	authRepo    *repository.AuthRepository
	memRepo     *repository.MemoryRepository
	workingRepo WorkingMemoryStore
	extractor   extractor.Extractor
}

type WorkingMemoryStore interface {
	Save(ctx context.Context, wm model.WorkingMemory) error
	Get(ctx context.Context, sessionID string) (*model.WorkingMemory, error)
}

func NewMemoryService(authRepo *repository.AuthRepository, memRepo *repository.MemoryRepository, workingRepo WorkingMemoryStore, ex extractor.Extractor) *MemoryService {
	return &MemoryService{authRepo: authRepo, memRepo: memRepo, workingRepo: workingRepo, extractor: ex}
}

type MemoryExtractRequest struct {
	UserID    string                   `json:"user_id"`
	SessionID string                   `json:"session_id"`
	Messages  []model.ConversationTurn `json:"messages"`
}

type MemoryExtractResponse struct {
	ExtractedFacts       []model.MemoryEntry `json:"extracted_facts"`
	ExtractedPreferences []model.MemoryEntry `json:"extracted_preferences"`
	ConversationSummary  string              `json:"conversation_summary"`
}

type MemoryRecallRequest struct {
	UserID    string `json:"user_id"`
	SessionID string `json:"session_id"`
	Query     string `json:"query"`
	TopK      int    `json:"top_k"`
}

type MemoryRecallResponse struct {
	Facts          []model.MemoryEntry  `json:"facts"`
	Preferences    []model.MemoryEntry  `json:"preferences"`
	WorkingMemory  *model.WorkingMemory `json:"working_memory"`
	ProfileSummary string               `json:"profile_summary"`
}

type UserProfile struct {
	UserID            string            `json:"user_id"`
	DisplayName       string            `json:"display_name"`
	Subject           string            `json:"subject"`
	School            string            `json:"school"`
	TeachingStyle     string            `json:"teaching_style"`
	ContentDepth      string            `json:"content_depth"`
	VisualPreferences map[string]string `json:"visual_preferences"`
	Preferences       map[string]string `json:"preferences"`
	HistorySummary    string            `json:"history_summary"`
	LastActiveAt      int64             `json:"last_active_at"`
}

type UpdateProfileRequest struct {
	DisplayName       string            `json:"display_name,omitempty"`
	Subject           string            `json:"subject,omitempty"`
	TeachingStyle     string            `json:"teaching_style,omitempty"`
	VisualPreferences map[string]string `json:"visual_preferences,omitempty"`
	Preferences       map[string]string `json:"preferences,omitempty"`
}

type SaveWorkingMemoryRequest struct {
	SessionID           string                 `json:"session_id"`
	UserID              string                 `json:"user_id"`
	ConversationSummary string                 `json:"conversation_summary"`
	ExtractedElements   model.TeachingElements `json:"extracted_elements"`
	RecentTopics        []string               `json:"recent_topics"`
}

func (s *MemoryService) Extract(ctx context.Context, req MemoryExtractRequest) (MemoryExtractResponse, error) {
	if strings.TrimSpace(req.UserID) == "" || len(req.Messages) == 0 {
		return MemoryExtractResponse{}, &ServiceError{Code: contract.CodeBadRequest, Message: "missing required field"}
	}
	res, err := s.extractor.Extract(req.UserID, req.SessionID, req.Messages)
	if err != nil {
		return MemoryExtractResponse{}, &ServiceError{Code: contract.CodeInternalError, Message: "internal error"}
	}
	sessionID := strings.TrimSpace(req.SessionID)
	var sourceSessionID *string
	if sessionID != "" {
		sourceSessionID = &sessionID
	}
	storedFacts := make([]model.MemoryEntry, 0, len(res.Facts))
	storedPrefs := make([]model.MemoryEntry, 0, len(res.Preferences))
	for _, f := range res.Facts {
		f.UserID = req.UserID
		f.Category = "fact"
		f.SourceSessionID = sourceSessionID
		if f.Context == "" {
			f.Context = "general"
		}
		stored, err := s.memRepo.UpsertMemoryEntry(f)
		if err != nil {
			return MemoryExtractResponse{}, &ServiceError{Code: contract.CodeInternalError, Message: "internal error"}
		}
		storedFacts = append(storedFacts, stored)
	}
	for _, p := range res.Preferences {
		p.UserID = req.UserID
		p.Category = "preference"
		p.SourceSessionID = sourceSessionID
		if p.Context == "" {
			p.Context = "general"
		}
		stored, err := s.memRepo.UpsertMemoryEntry(p)
		if err != nil {
			return MemoryExtractResponse{}, &ServiceError{Code: contract.CodeInternalError, Message: "internal error"}
		}
		storedPrefs = append(storedPrefs, stored)
	}
	summaryContext := "general"
	if sessionID != "" {
		summaryContext = "session:" + sessionID
	}
	_, err = s.memRepo.UpsertMemoryEntry(model.MemoryEntry{
		UserID:          req.UserID,
		Category:        "summary",
		Key:             "conversation_summary",
		Value:           res.ConversationSummary,
		Context:         summaryContext,
		Confidence:      1.0,
		Source:          "inferred",
		SourceSessionID: sourceSessionID,
	})
	if err != nil {
		return MemoryExtractResponse{}, &ServiceError{Code: contract.CodeInternalError, Message: "internal error"}
	}

	if sessionID != "" {
		wm := model.WorkingMemory{
			SessionID:           sessionID,
			UserID:              req.UserID,
			ConversationSummary: res.ConversationSummary,
			ExtractedElements:   res.TeachingElements(),
			UpdatedAt:           util.NowMilli(),
		}
		existing, err := s.workingRepo.Get(ctx, sessionID)
		if err == nil {
			wm.ExtractedElements = mergeTeachingElements(existing.ExtractedElements, wm.ExtractedElements)
			wm.RecentTopics = existing.RecentTopics
		}
		if err := s.workingRepo.Save(ctx, wm); err != nil {
			return MemoryExtractResponse{}, &ServiceError{Code: contract.CodeInternalError, Message: "internal error"}
		}
	}

	return MemoryExtractResponse{ExtractedFacts: storedFacts, ExtractedPreferences: storedPrefs, ConversationSummary: res.ConversationSummary}, nil
}

func (s *MemoryService) Recall(ctx context.Context, req MemoryRecallRequest) (MemoryRecallResponse, error) {
	if strings.TrimSpace(req.UserID) == "" || strings.TrimSpace(req.Query) == "" {
		return MemoryRecallResponse{}, &ServiceError{Code: contract.CodeBadRequest, Message: "missing required field"}
	}
	topK := req.TopK
	if topK <= 0 {
		topK = 10
	}
	facts, err := s.memRepo.ListMemoryByUserAndCategory(req.UserID, "fact")
	if err != nil {
		return MemoryRecallResponse{}, &ServiceError{Code: contract.CodeInternalError, Message: "internal error"}
	}
	prefs, err := s.memRepo.ListMemoryByUserAndCategory(req.UserID, "preference")
	if err != nil {
		return MemoryRecallResponse{}, &ServiceError{Code: contract.CodeInternalError, Message: "internal error"}
	}
	facts = rankEntries(facts, req.Query, topK)
	prefs = rankEntries(prefs, req.Query, topK)
	for i := range prefs {
		decayed := decayConfidence(prefs[i].Confidence, prefs[i].UpdatedAt)
		prefs[i].Confidence = decayed
		if decayed < 0.3 {
			prefs[i].Value = "[Low Confidence] " + prefs[i].Value
		}
	}
	var wm *model.WorkingMemory
	if strings.TrimSpace(req.SessionID) != "" {
		v, err := s.workingRepo.Get(ctx, strings.TrimSpace(req.SessionID))
		if err == nil {
			wm = v
		}
		if err != nil && err != repository.ErrNotFound {
			return MemoryRecallResponse{}, &ServiceError{Code: contract.CodeInternalError, Message: "internal error"}
		}
	}
	profile, err := s.GetProfile(req.UserID)
	if err != nil {
		return MemoryRecallResponse{}, err
	}
	profileSummary := buildProfileSummary(profile)
	return MemoryRecallResponse{Facts: facts, Preferences: prefs, WorkingMemory: wm, ProfileSummary: profileSummary}, nil
}

func (s *MemoryService) GetProfile(userID string) (UserProfile, error) {
	if strings.TrimSpace(userID) == "" {
		return UserProfile{}, &ServiceError{Code: contract.CodeBadRequest, Message: "missing required field"}
	}
	user, err := s.authRepo.GetUserByID(userID)
	if err != nil {
		if err == repository.ErrNotFound {
			return UserProfile{}, &ServiceError{Code: contract.CodeInvalidCredentials, Message: "invalid credentials"}
		}
		return UserProfile{}, &ServiceError{Code: contract.CodeInternalError, Message: "internal error"}
	}
	entries, err := s.memRepo.ListMemoryByUser(userID)
	if err != nil {
		return UserProfile{}, &ServiceError{Code: contract.CodeInternalError, Message: "internal error"}
	}
	profile := UserProfile{
		UserID:            user.ID,
		DisplayName:       user.DisplayName,
		Subject:           user.Subject,
		School:            user.School,
		VisualPreferences: map[string]string{},
		Preferences:       map[string]string{},
		LastActiveAt:      user.UpdatedAt,
	}
	for _, e := range entries {
		if e.UpdatedAt > profile.LastActiveAt {
			profile.LastActiveAt = e.UpdatedAt
		}
		if e.Category == "summary" && profile.HistorySummary == "" {
			profile.HistorySummary = e.Value
		}
		if e.Category != "preference" {
			continue
		}
		switch e.Key {
		case "teaching_style":
			profile.TeachingStyle = e.Value
		case "content_depth":
			profile.ContentDepth = e.Value
		default:
			if e.Context == "visual_preferences" {
				profile.VisualPreferences[e.Key] = e.Value
			} else {
				profile.Preferences[e.Key] = e.Value
			}
		}
	}
	return profile, nil
}

func (s *MemoryService) UpdateProfile(userID string, req UpdateProfileRequest) error {
	if strings.TrimSpace(userID) == "" {
		return &ServiceError{Code: contract.CodeBadRequest, Message: "missing required field"}
	}
	if err := s.memRepo.UpdateUserProfileFields(userID, req.DisplayName, req.Subject); err != nil {
		return &ServiceError{Code: contract.CodeInternalError, Message: "internal error"}
	}
	if strings.TrimSpace(req.TeachingStyle) != "" {
		if _, err := s.memRepo.UpsertMemoryEntry(model.MemoryEntry{UserID: userID, Category: "preference", Key: "teaching_style", Value: strings.TrimSpace(req.TeachingStyle), Context: "general", Confidence: 1.0, Source: "explicit"}); err != nil {
			return &ServiceError{Code: contract.CodeInternalError, Message: "internal error"}
		}
	}
	for k, v := range req.VisualPreferences {
		if strings.TrimSpace(k) == "" || strings.TrimSpace(v) == "" {
			continue
		}
		if _, err := s.memRepo.UpsertMemoryEntry(model.MemoryEntry{UserID: userID, Category: "preference", Key: strings.TrimSpace(k), Value: strings.TrimSpace(v), Context: "visual_preferences", Confidence: 1.0, Source: "explicit"}); err != nil {
			return &ServiceError{Code: contract.CodeInternalError, Message: "internal error"}
		}
	}
	for k, v := range req.Preferences {
		if strings.TrimSpace(k) == "" || strings.TrimSpace(v) == "" {
			continue
		}
		if _, err := s.memRepo.UpsertMemoryEntry(model.MemoryEntry{UserID: userID, Category: "preference", Key: strings.TrimSpace(k), Value: strings.TrimSpace(v), Context: "general", Confidence: 1.0, Source: "explicit"}); err != nil {
			return &ServiceError{Code: contract.CodeInternalError, Message: "internal error"}
		}
	}
	return nil
}

func (s *MemoryService) SaveWorkingMemory(ctx context.Context, req SaveWorkingMemoryRequest) error {
	if strings.TrimSpace(req.SessionID) == "" || strings.TrimSpace(req.UserID) == "" {
		return &ServiceError{Code: contract.CodeBadRequest, Message: "missing required field"}
	}
	wm := model.WorkingMemory{SessionID: strings.TrimSpace(req.SessionID), UserID: strings.TrimSpace(req.UserID), ConversationSummary: req.ConversationSummary, ExtractedElements: req.ExtractedElements, RecentTopics: req.RecentTopics, UpdatedAt: util.NowMilli()}
	if err := s.workingRepo.Save(ctx, wm); err != nil {
		return &ServiceError{Code: contract.CodeInternalError, Message: "internal error"}
	}
	return nil
}

func (s *MemoryService) GetWorkingMemory(ctx context.Context, sessionID string) (*model.WorkingMemory, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, &ServiceError{Code: contract.CodeBadRequest, Message: "missing required field"}
	}
	wm, err := s.workingRepo.Get(ctx, strings.TrimSpace(sessionID))
	if err == repository.ErrNotFound {
		return nil, &ServiceError{Code: contract.CodeResourceNotFound, Message: "resource not found"}
	}
	if err != nil {
		return nil, &ServiceError{Code: contract.CodeInternalError, Message: "internal error"}
	}
	return wm, nil
}

func rankEntries(entries []model.MemoryEntry, query string, topK int) []model.MemoryEntry {
	q := strings.ToLower(strings.TrimSpace(query))
	tokens := strings.Fields(q)
	type scored struct {
		e     model.MemoryEntry
		score int
	}
	scoredItems := make([]scored, 0, len(entries))
	for _, e := range entries {
		text := strings.ToLower(e.Key + " " + e.Value + " " + e.Context)
		score := 0
		if q != "" && strings.Contains(text, q) {
			score += 4
		}
		for _, t := range tokens {
			if strings.Contains(text, t) {
				score++
			}
		}
		scoredItems = append(scoredItems, scored{e: e, score: score})
	}
	sort.SliceStable(scoredItems, func(i, j int) bool {
		if scoredItems[i].score == scoredItems[j].score {
			return scoredItems[i].e.UpdatedAt > scoredItems[j].e.UpdatedAt
		}
		return scoredItems[i].score > scoredItems[j].score
	})
	if topK > len(scoredItems) {
		topK = len(scoredItems)
	}
	out := make([]model.MemoryEntry, 0, topK)
	for i := 0; i < topK; i++ {
		out = append(out, scoredItems[i].e)
	}
	return out
}

func mergeTeachingElements(existing, incoming model.TeachingElements) model.TeachingElements {
	out := existing
	out.KnowledgePoints = appendUniqueStrings(out.KnowledgePoints, incoming.KnowledgePoints...)
	out.TeachingGoals = appendUniqueStrings(out.TeachingGoals, incoming.TeachingGoals...)
	out.KeyDifficulties = appendUniqueStrings(out.KeyDifficulties, incoming.KeyDifficulties...)
	if strings.TrimSpace(incoming.TargetAudience) != "" {
		out.TargetAudience = strings.TrimSpace(incoming.TargetAudience)
	}
	if strings.TrimSpace(incoming.Duration) != "" {
		out.Duration = strings.TrimSpace(incoming.Duration)
	}
	if strings.TrimSpace(incoming.OutputStyle) != "" {
		out.OutputStyle = strings.TrimSpace(incoming.OutputStyle)
	}
	return out
}

func appendUniqueStrings(base []string, incoming ...string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(base)+len(incoming))
	for _, item := range append(append([]string{}, base...), incoming...) {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		key := strings.ToLower(item)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}

func decayConfidence(conf float64, updatedAt int64) float64 {
	if conf <= 0 {
		return 0
	}
	days := float64(util.NowMilli()-updatedAt) / float64(24*60*60*1000)
	if days < 30 {
		return conf
	}
	steps := math.Floor(days / 30)
	return conf * math.Pow(0.9, steps)
}

func buildProfileSummary(p UserProfile) string {
	parts := make([]string, 0, 4)
	if p.Subject != "" {
		parts = append(parts, fmt.Sprintf("Subject: %s", p.Subject))
	}
	if p.TeachingStyle != "" {
		parts = append(parts, fmt.Sprintf("Teaching style: %s", p.TeachingStyle))
	}
	if p.ContentDepth != "" {
		parts = append(parts, fmt.Sprintf("Content depth: %s", p.ContentDepth))
	}
	if p.HistorySummary != "" {
		parts = append(parts, p.HistorySummary)
	}
	return strings.Join(parts, "; ")
}
