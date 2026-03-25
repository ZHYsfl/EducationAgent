package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"zcxppt/internal/model"
)

type RedisPPTRepository struct {
	client *redis.Client
}

func NewRedisPPTRepository(client *redis.Client) *RedisPPTRepository {
	return &RedisPPTRepository{client: client}
}

func (r *RedisPPTRepository) canvasKey(taskID string) string {
	return "canvas:latest:" + taskID
}

func (r *RedisPPTRepository) pageKey(taskID, pageID string) string {
	return "canvas:page:" + taskID + ":" + pageID
}

func (r *RedisPPTRepository) snapshotKey(taskID string, ts int64) string {
	return fmt.Sprintf("snapshot:%s:%d", taskID, ts)
}

func (r *RedisPPTRepository) InitCanvas(taskID string) (model.CanvasStatusResponse, error) {
	ctx := context.Background()
	now := time.Now().UnixMilli()
	pageIDs := []string{"page_" + uuid.NewString(), "page_" + uuid.NewString()}
	pagesInfo := []model.PageStatusInfo{
		{PageID: pageIDs[0], Status: "completed", LastUpdate: now, RenderURL: ""},
		{PageID: pageIDs[1], Status: "completed", LastUpdate: now, RenderURL: ""},
	}
	canvas := model.CanvasStatusResponse{
		TaskID:               taskID,
		PageOrder:            pageIDs,
		CurrentViewingPageID: pageIDs[0],
		PagesInfo:            pagesInfo,
	}
	if err := r.setCanvas(ctx, canvas); err != nil {
		return model.CanvasStatusResponse{}, err
	}
	for _, id := range pageIDs {
		page := model.PageRenderResponse{
			TaskID:    taskID,
			PageID:    id,
			Status:    "completed",
			RenderURL: "",
			PyCode:    "# mock pyppt page code",
			Version:   1,
			UpdatedAt: now,
		}
		if err := r.setPage(ctx, page); err != nil {
			return model.CanvasStatusResponse{}, err
		}
	}
	_ = r.saveSnapshot(ctx, taskID, canvas)
	return canvas, nil
}

func (r *RedisPPTRepository) GetCanvasStatus(taskID string) (model.CanvasStatusResponse, error) {
	ctx := context.Background()
	v, err := r.client.Get(ctx, r.canvasKey(taskID)).Result()
	if err == redis.Nil {
		return model.CanvasStatusResponse{}, ErrTaskNotFound
	}
	if err != nil {
		return model.CanvasStatusResponse{}, err
	}
	var out model.CanvasStatusResponse
	if err := json.Unmarshal([]byte(v), &out); err != nil {
		return model.CanvasStatusResponse{}, err
	}
	return out, nil
}

func (r *RedisPPTRepository) GetPageRender(taskID, pageID string) (model.PageRenderResponse, error) {
	ctx := context.Background()
	v, err := r.client.Get(ctx, r.pageKey(taskID, pageID)).Result()
	if err == redis.Nil {
		return model.PageRenderResponse{}, ErrPageNotFound
	}
	if err != nil {
		return model.PageRenderResponse{}, err
	}
	var out model.PageRenderResponse
	if err := json.Unmarshal([]byte(v), &out); err != nil {
		return model.PageRenderResponse{}, err
	}
	return out, nil
}

func (r *RedisPPTRepository) UpdatePageCode(taskID, pageID, pyCode, renderURL string) (model.PageRenderResponse, error) {
	ctx := context.Background()
	page, err := r.GetPageRender(taskID, pageID)
	if err != nil {
		return model.PageRenderResponse{}, err
	}
	page.PyCode = pyCode
	if renderURL != "" {
		page.RenderURL = renderURL
	}
	page.Status = "completed"
	page.UpdatedAt = time.Now().UnixMilli()
	page.Version++
	if err := r.setPage(ctx, page); err != nil {
		return model.PageRenderResponse{}, err
	}

	canvas, err := r.GetCanvasStatus(taskID)
	if err != nil {
		return model.PageRenderResponse{}, err
	}
	for i := range canvas.PagesInfo {
		if canvas.PagesInfo[i].PageID == pageID {
			canvas.PagesInfo[i].Status = "completed"
			canvas.PagesInfo[i].RenderURL = page.RenderURL
			canvas.PagesInfo[i].LastUpdate = page.UpdatedAt
		}
	}
	if err := r.setCanvas(ctx, canvas); err != nil {
		return model.PageRenderResponse{}, err
	}
	_ = r.saveSnapshot(ctx, taskID, canvas)
	return page, nil
}

func (r *RedisPPTRepository) setCanvas(ctx context.Context, canvas model.CanvasStatusResponse) error {
	b, err := json.Marshal(canvas)
	if err != nil {
		return err
	}
	return r.client.Set(ctx, r.canvasKey(canvas.TaskID), b, 24*time.Hour).Err()
}

func (r *RedisPPTRepository) setPage(ctx context.Context, page model.PageRenderResponse) error {
	b, err := json.Marshal(page)
	if err != nil {
		return err
	}
	return r.client.Set(ctx, r.pageKey(page.TaskID, page.PageID), b, 24*time.Hour).Err()
}

func (r *RedisPPTRepository) saveSnapshot(ctx context.Context, taskID string, canvas model.CanvasStatusResponse) error {
	b, err := json.Marshal(canvas)
	if err != nil {
		return err
	}
	return r.client.Set(ctx, r.snapshotKey(taskID, time.Now().UnixMilli()), b, 300*time.Second).Err()
}
