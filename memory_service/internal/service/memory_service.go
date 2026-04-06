package service

import (
	"context"
	"strings"

	"memory_service/internal/contract"
	"memory_service/internal/infra/extractor"
	"memory_service/internal/model"
	"memory_service/internal/repository"
	"memory_service/internal/util"
)

type MemoryService struct {
	authRepo    *repository.AuthRepository
	memRepo     *repository.MemoryRepository
	workingRepo WorkingMemoryStore
	extractor   extractor.Extractor
}

type WorkingMemoryStore interface {
	Save(ctx context.Context, wm model.WorkingMemoryRecord) error
	Get(ctx context.Context, sessionID string) (*model.WorkingMemoryRecord, error)
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
	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID != "" {
		if err := s.ensureSessionOwnership(ctx, sessionID, req.UserID); err != nil {
			return MemoryExtractResponse{}, err
		}
	}
	res, err := s.extractor.Extract(req.UserID, req.SessionID, req.Messages)
	if err != nil {
		return MemoryExtractResponse{}, &ServiceError{Code: contract.CodeInternalError, Message: "internal error"}
	}
	var sourceSessionID *string
	if sessionID != "" {
		sourceSessionID = &sessionID
	}
	durableFacts, durablePrefs := classifyDurableWrites(req.Messages, res.Facts, res.Preferences)
	storedFacts := make([]model.MemoryEntry, 0, len(durableFacts))
	storedPrefs := make([]model.MemoryEntry, 0, len(durablePrefs))
	for _, f := range durableFacts {
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
	for _, p := range durablePrefs {
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
	signals := mergeTaskSignals(signalsFromTeachingElements(res.TeachingElements(), model.SlotProvenanceDerived), res.TaskStateSignals())
	nowMs := util.NowMilli()
	summaryText := buildWorkingSummary(model.WorkingTaskState{
		LessonTopic:            signals.LessonTopic,
		KnowledgePoints:        signals.KnowledgePoints,
		TeachingGoals:          signals.TeachingGoals,
		KeyDifficulties:        signals.KeyDifficulties,
		TargetAudience:         signals.TargetAudience,
		Duration:               signals.Duration,
		OutputStyle:            signals.OutputStyle,
		TeachingLogic:          signals.TeachingLogic,
		Constraints:            signals.Constraints,
		ReferenceMaterialUsage: signals.ReferenceMaterialUsage,
	}, res.ConversationSummary)
	if sessionID != "" {
		var existing *model.WorkingMemoryRecord
		existing, err = s.workingRepo.Get(ctx, sessionID)
		if err != nil && err != repository.ErrNotFound {
			return MemoryExtractResponse{}, &ServiceError{Code: contract.CodeInternalError, Message: "internal error"}
		}
		record := mergeWorkingMemoryRecord(existing, sessionID, req.UserID, signals, res.ConversationSummary, nil, nowMs)
		summaryText = projectWorkingMemoryRecord(record).ConversationSummary
		if err := s.workingRepo.Save(ctx, record); err != nil {
			return MemoryExtractResponse{}, &ServiceError{Code: contract.CodeInternalError, Message: "internal error"}
		}
	}
	summaryContext := "general"
	if sessionID != "" {
		summaryContext = "session:" + sessionID
	}
	_, err = s.memRepo.UpsertMemoryEntry(model.MemoryEntry{
		UserID:          req.UserID,
		Category:        "summary",
		Key:             "conversation_summary",
		Value:           summaryText,
		Context:         summaryContext,
		Confidence:      1.0,
		Source:          "inferred",
		SourceSessionID: sourceSessionID,
	})
	if err != nil {
		return MemoryExtractResponse{}, &ServiceError{Code: contract.CodeInternalError, Message: "internal error"}
	}

<<<<<<< HEAD
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
=======
	return MemoryExtractResponse{ExtractedFacts: storedFacts, ExtractedPreferences: storedPrefs, ConversationSummary: summaryText}, nil
>>>>>>> origin/wang
}

func (s *MemoryService) Recall(ctx context.Context, req MemoryRecallRequest) (MemoryRecallResponse, error) {
	if strings.TrimSpace(req.UserID) == "" || strings.TrimSpace(req.Query) == "" {
		return MemoryRecallResponse{}, &ServiceError{Code: contract.CodeBadRequest, Message: "missing required field"}
	}
	topK := req.TopK
	if topK <= 0 {
		topK = 10
	}
	sessionID := strings.TrimSpace(req.SessionID)
	facts, err := s.memRepo.ListMemoryByUserAndCategory(req.UserID, "fact")
	if err != nil {
		return MemoryRecallResponse{}, &ServiceError{Code: contract.CodeInternalError, Message: "internal error"}
	}
	prefs, err := s.memRepo.ListMemoryByUserAndCategory(req.UserID, "preference")
	if err != nil {
		return MemoryRecallResponse{}, &ServiceError{Code: contract.CodeInternalError, Message: "internal error"}
	}
	var wm *model.WorkingMemory
	if sessionID != "" {
<<<<<<< HEAD
=======
		if err := s.ensureSessionOwnership(ctx, sessionID, req.UserID); err != nil {
			return MemoryRecallResponse{}, err
		}
>>>>>>> origin/wang
		v, err := s.workingRepo.Get(ctx, sessionID)
		if err == nil {
			projected := projectWorkingMemoryRecord(*v)
			wm = &projected
		}
		if err != nil && err != repository.ErrNotFound {
			return MemoryRecallResponse{}, &ServiceError{Code: contract.CodeInternalError, Message: "internal error"}
		}
	}
	q := normalizeQuery(req.Query)
	hints := detectIntentHints(q, sessionID != "")
	ws := extractWorkingSignals(wm, sessionID)
	nowMs := util.NowMilli()

	factCandidates := make([]ScoredCandidate, 0, len(facts))
	for _, fact := range facts {
		factCandidates = append(factCandidates, scoreCandidate(fact, "fact", q, hints, ws, nowMs))
	}
	prefCandidates := make([]ScoredCandidate, 0, len(prefs))
	for _, pref := range prefs {
		decayed := applyPreferenceDecay(pref, nowMs)
		prefCandidates = append(prefCandidates, scoreCandidate(decayed, "preference", q, hints, ws, nowMs))
	}

	minScore := recallMinScore
	if len(q.Tokens) == 0 && q.Normalized == "" {
		minScore = 0
	}
	factCandidates = filterByMinScore(factCandidates, minScore)
	prefCandidates = filterByMinScore(prefCandidates, minScore)
	factCandidates = rankCandidates(factCandidates)
	prefCandidates = rankCandidates(prefCandidates)
	budget := buildBudget(topK, hints, len(factCandidates), len(prefCandidates))
	selectedFacts, selectedPrefs := selectWithBudget(factCandidates, prefCandidates, budget)

	profile, err := s.GetProfile(req.UserID)
	if err != nil {
		return MemoryRecallResponse{}, err
	}
	profileSummary := composeProfileSummary(profile, wm, hints)
	return MemoryRecallResponse{
		Facts:          selectedFacts,
		Preferences:    selectedPrefs,
		WorkingMemory:  wm,
		ProfileSummary: profileSummary,
	}, nil
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
	sessionID := strings.TrimSpace(req.SessionID)
	userID := strings.TrimSpace(req.UserID)
	if err := s.ensureSessionOwnership(ctx, sessionID, userID); err != nil {
		return err
	}
	var existing *model.WorkingMemoryRecord
	existing, err := s.workingRepo.Get(ctx, sessionID)
	if err != nil && err != repository.ErrNotFound {
		return &ServiceError{Code: contract.CodeInternalError, Message: "internal error"}
	}
	signals := signalsFromTeachingElements(req.ExtractedElements, model.SlotProvenanceDerived)
	record := mergeWorkingMemoryRecord(existing, sessionID, userID, signals, req.ConversationSummary, req.RecentTopics, util.NowMilli())
	if err := s.workingRepo.Save(ctx, record); err != nil {
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
	projected := projectWorkingMemoryRecord(*wm)
	return &projected, nil
}

<<<<<<< HEAD
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
=======
func (s *MemoryService) ensureSessionOwnership(ctx context.Context, sessionID, userID string) *ServiceError {
	sessionID = strings.TrimSpace(sessionID)
	userID = strings.TrimSpace(userID)
	if sessionID == "" || userID == "" {
		return nil
	}
	record, err := s.workingRepo.Get(ctx, sessionID)
	if err == nil {
		if strings.TrimSpace(record.UserID) != "" && strings.TrimSpace(record.UserID) != userID {
			return &ServiceError{Code: contract.CodeInvalidCredentials, Message: "invalid credentials"}
		}
		return nil
	}
	if err != repository.ErrNotFound {
		return &ServiceError{Code: contract.CodeInternalError, Message: "internal error"}
	}
	session, err := s.authRepo.GetSessionByID(sessionID)
	if err == repository.ErrNotFound {
		return nil
	}
	if err != nil {
		return &ServiceError{Code: contract.CodeInternalError, Message: "internal error"}
	}
	if strings.TrimSpace(session.UserID) != "" && strings.TrimSpace(session.UserID) != userID {
		return &ServiceError{Code: contract.CodeInvalidCredentials, Message: "invalid credentials"}
	}
	return nil
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
>>>>>>> origin/wang
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
