package repository

import (
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"

	"zcxppt/internal/model"
)

var ErrPageNotFound = errors.New("page not found")

type PPTRepository interface {
	InitCanvas(taskID string) (model.CanvasStatusResponse, error)
	GetCanvasStatus(taskID string) (model.CanvasStatusResponse, error)
	GetPageRender(taskID, pageID string) (model.PageRenderResponse, error)
	UpdatePageCode(taskID, pageID, pyCode, renderURL string) (model.PageRenderResponse, error)
}

type InMemoryPPTRepository struct {
	mu       sync.RWMutex
	canvases map[string]model.CanvasStatusResponse
	pages    map[string]map[string]model.PageRenderResponse
}

func NewInMemoryPPTRepository() *InMemoryPPTRepository {
	return &InMemoryPPTRepository{
		canvases: make(map[string]model.CanvasStatusResponse),
		pages:    make(map[string]map[string]model.PageRenderResponse),
	}
}

func (r *InMemoryPPTRepository) InitCanvas(taskID string) (model.CanvasStatusResponse, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

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
	r.canvases[taskID] = canvas

	if _, ok := r.pages[taskID]; !ok {
		r.pages[taskID] = make(map[string]model.PageRenderResponse)
	}
	for _, id := range pageIDs {
		r.pages[taskID][id] = model.PageRenderResponse{
			TaskID:    taskID,
			PageID:    id,
			Status:    "completed",
			RenderURL: "",
			PyCode:    "# mock pyppt page code",
			Version:   1,
			UpdatedAt: now,
		}
	}
	return canvas, nil
}

func (r *InMemoryPPTRepository) GetCanvasStatus(taskID string) (model.CanvasStatusResponse, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	canvas, ok := r.canvases[taskID]
	if !ok {
		return model.CanvasStatusResponse{}, ErrTaskNotFound
	}
	return canvas, nil
}

func (r *InMemoryPPTRepository) GetPageRender(taskID, pageID string) (model.PageRenderResponse, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	pages, ok := r.pages[taskID]
	if !ok {
		return model.PageRenderResponse{}, ErrTaskNotFound
	}
	page, ok := pages[pageID]
	if !ok {
		return model.PageRenderResponse{}, ErrPageNotFound
	}
	return page, nil
}

func (r *InMemoryPPTRepository) UpdatePageCode(taskID, pageID, pyCode, renderURL string) (model.PageRenderResponse, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	pages, ok := r.pages[taskID]
	if !ok {
		return model.PageRenderResponse{}, ErrTaskNotFound
	}
	page, ok := pages[pageID]
	if !ok {
		return model.PageRenderResponse{}, ErrPageNotFound
	}
	page.PyCode = pyCode
	if renderURL != "" {
		page.RenderURL = renderURL
	}
	page.Status = "completed"
	page.UpdatedAt = time.Now().UnixMilli()
	page.Version++
	pages[pageID] = page

	canvas := r.canvases[taskID]
	for i := range canvas.PagesInfo {
		if canvas.PagesInfo[i].PageID == pageID {
			canvas.PagesInfo[i].RenderURL = page.RenderURL
			canvas.PagesInfo[i].Status = "completed"
			canvas.PagesInfo[i].LastUpdate = page.UpdatedAt
		}
	}
	r.canvases[taskID] = canvas
	return page, nil
}
