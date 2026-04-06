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

func (r *RedisPPTRepository) pageToTaskKey(pageID string) string {
	return "canvas:page:" + pageID
}

func (r *RedisPPTRepository) InitCanvas(taskID string, totalPages int) (model.CanvasStatusResponse, error) {
	ctx := context.Background()
	now := time.Now().UnixMilli()

	if totalPages < 1 {
		totalPages = 1
	}

	pageIDs := make([]string, 0, totalPages)
	for i := 0; i < totalPages; i++ {
		pageIDs = append(pageIDs, "page_"+uuid.NewString())
	}

	pagesInfo := make([]model.PageStatusInfo, 0, totalPages)
	for _, id := range pageIDs {
		pagesInfo = append(pagesInfo, model.PageStatusInfo{
			PageID:     id,
			Status:     "rendering",
			LastUpdate: now,
			RenderURL:  "",
		})
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
			Status:    "rendering",
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

func (r *RedisPPTRepository) SetCurrentViewingPageID(taskID, pageID string) error {
	ctx := context.Background()
	canvas, err := r.GetCanvasStatus(taskID)
	if err != nil {
		return err
	}
	found := false
	for _, pid := range canvas.PageOrder {
		if pid == pageID {
			found = true
			break
		}
	}
	if !found {
		return ErrPageNotFound
	}
	canvas.CurrentViewingPageID = pageID
	if err := r.setCanvas(ctx, canvas); err != nil {
		return err
	}
	_ = r.saveSnapshot(ctx, taskID, canvas)
	return nil
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

func (r *RedisPPTRepository) UpdatePageStatus(taskID, pageID, status, errorMsg string) error {
	ctx := context.Background()
	page, err := r.GetPageRender(taskID, pageID)
	if err != nil {
		return err
	}
	page.Status = status
	page.ErrorMsg = errorMsg
	page.UpdatedAt = time.Now().UnixMilli()
	if err := r.setPage(ctx, page); err != nil {
		return err
	}

	canvas, err := r.GetCanvasStatus(taskID)
	if err != nil {
		return err
	}
	for i := range canvas.PagesInfo {
		if canvas.PagesInfo[i].PageID == pageID {
			canvas.PagesInfo[i].Status = status
			canvas.PagesInfo[i].LastUpdate = page.UpdatedAt
		}
	}
	if err := r.setCanvas(ctx, canvas); err != nil {
		return err
	}
	_ = r.saveSnapshot(ctx, taskID, canvas)
	return nil
}

func (r *RedisPPTRepository) InsertPageAfter(taskID, afterPageID string, newPage model.PageRenderResponse) error {
	ctx := context.Background()
	canvas, err := r.GetCanvasStatus(taskID)
	if err != nil {
		return err
	}
	pos := -1
	for i, pid := range canvas.PageOrder {
		if pid == afterPageID {
			pos = i + 1
			break
		}
	}
	if pos < 0 {
		return ErrPageNotFound
	}

	canvas.PageOrder = append(canvas.PageOrder[:pos], append([]string{newPage.PageID}, canvas.PageOrder[pos:]...)...)
	canvas.PagesInfo = append(canvas.PagesInfo[:pos], append([]model.PageStatusInfo{{PageID: newPage.PageID, Status: newPage.Status, LastUpdate: newPage.UpdatedAt, RenderURL: newPage.RenderURL}}, canvas.PagesInfo[pos:]...)...)

	if err := r.setPage(ctx, newPage); err != nil {
		return err
	}
	if err := r.setCanvas(ctx, canvas); err != nil {
		return err
	}
	_ = r.saveSnapshot(ctx, taskID, canvas)
	return nil
}

func (r *RedisPPTRepository) InsertPageBefore(taskID, beforePageID string, newPage model.PageRenderResponse) error {
	ctx := context.Background()
	canvas, err := r.GetCanvasStatus(taskID)
	if err != nil {
		return err
	}
	pos := -1
	for i, pid := range canvas.PageOrder {
		if pid == beforePageID {
			pos = i
			break
		}
	}
	if pos < 0 {
		return ErrPageNotFound
	}

	canvas.PageOrder = append(canvas.PageOrder[:pos], append([]string{newPage.PageID}, canvas.PageOrder[pos:]...)...)
	canvas.PagesInfo = append(canvas.PagesInfo[:pos], append([]model.PageStatusInfo{{PageID: newPage.PageID, Status: newPage.Status, LastUpdate: newPage.UpdatedAt, RenderURL: newPage.RenderURL}}, canvas.PagesInfo[pos:]...)...)

	if err := r.setPage(ctx, newPage); err != nil {
		return err
	}
	if err := r.setCanvas(ctx, canvas); err != nil {
		return err
	}
	_ = r.saveSnapshot(ctx, taskID, canvas)
	return nil
}

func (r *RedisPPTRepository) DeletePage(taskID, pageID string) error {
	ctx := context.Background()
	canvas, err := r.GetCanvasStatus(taskID)
	if err != nil {
		return err
	}
	found := false
	for _, pid := range canvas.PageOrder {
		if pid == pageID {
			found = true
			break
		}
	}
	if !found {
		return ErrPageNotFound
	}

	newOrder := make([]string, 0, len(canvas.PageOrder)-1)
	for _, pid := range canvas.PageOrder {
		if pid != pageID {
			newOrder = append(newOrder, pid)
		}
	}
	newPagesInfo := make([]model.PageStatusInfo, 0, len(canvas.PagesInfo)-1)
	for _, pi := range canvas.PagesInfo {
		if pi.PageID != pageID {
			newPagesInfo = append(newPagesInfo, pi)
		}
	}
	if len(newOrder) > 0 {
		canvas.CurrentViewingPageID = newOrder[0]
	}
	canvas.PageOrder = newOrder
	canvas.PagesInfo = newPagesInfo

	if err := r.client.Del(ctx, r.pageKey(taskID, pageID)).Err(); err != nil {
		return err
	}
	if err := r.setCanvas(ctx, canvas); err != nil {
		return err
	}
	_ = r.saveSnapshot(ctx, taskID, canvas)
	return nil
}

func (r *RedisPPTRepository) GetTaskIDByPageID(pageID string) (string, error) {
	ctx := context.Background()
	pattern := "canvas:page:*:" + pageID
	iter := r.client.Scan(ctx, 0, pattern, 1).Iterator()
	for iter.Next(ctx) {
		key := iter.Val()
		if len(key) > len("canvas:page:") {
			taskID := key[len("canvas:page:"):len(key)-len(pageID)-1]
			return taskID, nil
		}
	}
	_ = iter.Err()
	return "", ErrPageNotFound
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
