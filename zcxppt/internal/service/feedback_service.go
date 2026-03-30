package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"zcxppt/internal/infra/llm"
	"zcxppt/internal/model"
	"zcxppt/internal/repository"
)

var (
	ErrInvalidReplyContext = errors.New("reply_to_context_id is required for resolve_conflict")
	ErrContextNotMatched   = errors.New("reply_to_context_id does not match suspended context")
	ErrNoSuspendedConflict = errors.New("resolve_conflict requires an active suspended conflict")
	ErrInvalidBatchGenerateRequest = errors.New("invalid batch generate request")
)

type FeedbackService struct {
	pptRepo      repository.PPTRepository
	feedbackRepo repository.FeedbackRepository
	llmRuntime   llm.ToolRuntime
	notify       *NotifyService
}

func NewFeedbackService(
	pptRepo repository.PPTRepository,
	feedbackRepo repository.FeedbackRepository,
	llmRuntime llm.ToolRuntime,
	notify *NotifyService,
) *FeedbackService {
	return &FeedbackService{pptRepo: pptRepo, feedbackRepo: feedbackRepo, llmRuntime: llmRuntime, notify: notify}
}

func (s *FeedbackService) Handle(ctx context.Context, req model.FeedbackRequest) (model.FeedbackResponse, error) {
	resolveConflict := hasResolveConflictIntent(req.Intents)
	if resolveConflict && strings.TrimSpace(req.ReplyToContextID) == "" {
		return model.FeedbackResponse{}, ErrInvalidReplyContext
	}

	current, err := s.pptRepo.GetPageRender(req.TaskID, req.ViewingPageID)
	if err != nil {
		return model.FeedbackResponse{}, err
	}

	suspend, suspended, _ := s.feedbackRepo.GetSuspend(req.TaskID, req.ViewingPageID)
	if suspended && !suspend.Resolved {
		if resolveConflict {
			if strings.TrimSpace(req.ReplyToContextID) != suspend.ContextID {
				return model.FeedbackResponse{}, ErrContextNotMatched
			}
			_ = s.feedbackRepo.ResolveSuspend(req.TaskID, req.ViewingPageID)
		} else {
			pending := model.PendingFeedback{
				TaskID:        req.TaskID,
				PageID:        req.ViewingPageID,
				BaseTimestamp: req.BaseTimestamp,
				RawText:       req.RawText,
				Intents:       req.Intents,
				CreatedAt:     time.Now().UnixMilli(),
			}
			_ = s.feedbackRepo.EnqueuePending(req.TaskID, req.ViewingPageID, pending)
			_ = s.notify.SendPPTMessage(ctx, map[string]any{
				"task_id":    req.TaskID,
				"page_id":    req.ViewingPageID,
				"priority":   "high",
				"context_id": suspend.ContextID,
				"tts_text":   suspend.Question,
				"msg_type":   "conflict_question",
			})
			return model.FeedbackResponse{AcceptedIntents: len(req.Intents), Queued: true}, nil
		}
	} else if resolveConflict {
		return model.FeedbackResponse{}, ErrNoSuspendedConflict
	}

	mergeResult, err := s.llmRuntime.RunFeedbackLoop(ctx, req, current)
	if err != nil {
		return model.FeedbackResponse{}, err
	}
	if mergeResult.MergeStatus == "ask_human" {
		contextID := "ctx_" + uuid.NewString()
		suspend := model.SuspendState{
			TaskID:     req.TaskID,
			PageID:     req.ViewingPageID,
			ContextID:  contextID,
			Question:   mergeResult.QuestionForUser,
			RetryCount: 0,
			CreatedAt:  time.Now().UnixMilli(),
			ExpiresAt:  time.Now().Add(45 * time.Second).UnixMilli(),
			Resolved:   false,
		}
		_ = s.feedbackRepo.SetSuspend(suspend)
		_ = s.notify.SendPPTMessage(ctx, map[string]any{
			"task_id":    req.TaskID,
			"page_id":    req.ViewingPageID,
			"priority":   "high",
			"context_id": contextID,
			"tts_text":   mergeResult.QuestionForUser,
			"msg_type":   "conflict_question",
		})
		return model.FeedbackResponse{AcceptedIntents: len(req.Intents), Queued: true}, nil
	}

	_, err = s.pptRepo.UpdatePageCode(req.TaskID, req.ViewingPageID, mergeResult.MergedPyCode, current.RenderURL)
	if err != nil {
		return model.FeedbackResponse{}, err
	}
	return model.FeedbackResponse{AcceptedIntents: len(req.Intents), Queued: false}, nil
}

func hasResolveConflictIntent(intents []model.Intent) bool {
	for _, it := range intents {
		if strings.EqualFold(strings.TrimSpace(it.ActionType), "resolve_conflict") {
			return true
		}
	}
	return false
}

func (s *FeedbackService) ProcessTimeoutTick(ctx context.Context) error {
	expired, err := s.feedbackRepo.ListExpiredSuspends(time.Now())
	if err != nil {
		return err
	}
	for _, item := range expired {
		if item.RetryCount < 3 {
			item.RetryCount++
			item.ExpiresAt = time.Now().Add(45 * time.Second).UnixMilli()
			_ = s.feedbackRepo.SetSuspend(item)
			_ = s.notify.SendPPTMessage(ctx, map[string]any{
				"task_id":    item.TaskID,
				"page_id":    item.PageID,
				"priority":   "high",
				"context_id": item.ContextID,
				"tts_text":   item.Question,
				"msg_type":   "conflict_question",
			})
			continue
		}

		_ = s.feedbackRepo.ResolveSuspend(item.TaskID, item.PageID)
		pending, ok, _ := s.feedbackRepo.DequeuePending(item.TaskID, item.PageID)
		if ok {
			_, _ = s.Handle(ctx, model.FeedbackRequest{
				TaskID:        pending.TaskID,
				ViewingPageID: pending.PageID,
				BaseTimestamp: pending.BaseTimestamp,
				RawText:       pending.RawText,
				Intents:       pending.Intents,
			})
		}
	}
	return nil
}

// GeneratePages runs merge+write for multiple pages concurrently.
// Current project does not have a real "renderer", so this method writes a mock render_url.
func (s *FeedbackService) GeneratePages(ctx context.Context, req model.BatchGeneratePagesRequest) (model.BatchGeneratePagesResponse, error) {
	taskID := strings.TrimSpace(req.TaskID)
	if taskID == "" || req.BaseTimestamp <= 0 || strings.TrimSpace(req.RawText) == "" {
		return model.BatchGeneratePagesResponse{}, ErrInvalidBatchGenerateRequest
	}

	var pageIDs []string
	if len(req.PageIDs) > 0 {
		pageIDs = make([]string, 0, len(req.PageIDs))
		for _, id := range req.PageIDs {
			id = strings.TrimSpace(id)
			if id != "" {
				pageIDs = append(pageIDs, id)
			}
		}
	} else {
		canvas, err := s.pptRepo.GetCanvasStatus(taskID)
		if err != nil {
			return model.BatchGeneratePagesResponse{}, err
		}
		pageIDs = canvas.PageOrder
	}
	if len(pageIDs) == 0 {
		return model.BatchGeneratePagesResponse{}, ErrInvalidBatchGenerateRequest
	}

	maxParallel := req.MaxParallel
	if maxParallel <= 0 {
		maxParallel = 4
	}
	if maxParallel > len(pageIDs) {
		maxParallel = len(pageIDs)
	}
	if maxParallel <= 0 {
		maxParallel = 1
	}

	results := make([]model.BatchGeneratePageResult, len(pageIDs))
	sem := make(chan struct{}, maxParallel)
	var wg sync.WaitGroup
	var mu sync.Mutex

	for i, pageID := range pageIDs {
		i := i
		pageID := pageID

		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			current, err := s.pptRepo.GetPageRender(taskID, pageID)
			if err != nil {
				mu.Lock()
				results[i] = model.BatchGeneratePageResult{
					PageID: pageID, Status: "failed", Error: err.Error(),
				}
				mu.Unlock()
				return
			}

			perPageIntents := make([]model.Intent, len(req.Intents))
			for j := range req.Intents {
				perPageIntents[j] = req.Intents[j]
				perPageIntents[j].TargetPageID = pageID
			}

			fbReq := model.FeedbackRequest{
				TaskID:           taskID,
				BaseTimestamp:    req.BaseTimestamp,
				ViewingPageID:    pageID,
				ReplyToContextID: "",
				RawText:          req.RawText,
				Intents:          perPageIntents,
			}

			mergeResult, err := s.llmRuntime.RunFeedbackLoop(ctx, fbReq, current)
			if err != nil {
				mu.Lock()
				results[i] = model.BatchGeneratePageResult{
					PageID: pageID, Status: "failed", Error: err.Error(),
				}
				mu.Unlock()
				return
			}

			if mergeResult.MergeStatus != "auto_resolved" {
				mu.Lock()
				results[i] = model.BatchGeneratePageResult{
					PageID: pageID, Status: "failed", Error: mergeResult.QuestionForUser,
				}
				mu.Unlock()
				return
			}

			renderURL := strings.TrimSpace(current.RenderURL)
			if renderURL == "" {
				renderURL = fmt.Sprintf("mock://render/%s/%s", taskID, pageID)
			}
			updated, err := s.pptRepo.UpdatePageCode(taskID, pageID, mergeResult.MergedPyCode, renderURL)
			if err != nil {
				mu.Lock()
				results[i] = model.BatchGeneratePageResult{
					PageID: pageID, Status: "failed", RenderURL: renderURL, Error: err.Error(),
				}
				mu.Unlock()
				return
			}

			mu.Lock()
			results[i] = model.BatchGeneratePageResult{
				PageID:    pageID,
				Status:    "completed",
				RenderURL: updated.RenderURL,
				Version:   updated.Version,
			}
			mu.Unlock()
		}()
	}

	wg.Wait()
	return model.BatchGeneratePagesResponse{
		TaskID:  taskID,
		Results: results,
	}, nil
}
