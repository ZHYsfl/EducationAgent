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
	InitCanvas(taskID string, totalPages int) (model.CanvasStatusResponse, error)
	GetCanvasStatus(taskID string) (model.CanvasStatusResponse, error)
	GetPageRender(taskID, pageID string) (model.PageRenderResponse, error)
	UpdatePageCode(taskID, pageID, pyCode, renderURL string) (model.PageRenderResponse, error)
	InsertPageAfter(taskID, afterPageID string, newPage model.PageRenderResponse) error
	InsertPageBefore(taskID, beforePageID string, newPage model.PageRenderResponse) error
	DeletePage(taskID, pageID string) error
	GetTaskIDByPageID(pageID string) (string, error)
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

func (r *InMemoryPPTRepository) InitCanvas(taskID string, totalPages int) (model.CanvasStatusResponse, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if totalPages < 1 {
		totalPages = 1
	}

	now := time.Now().UnixMilli()

	pageIDs := make([]string, 0, totalPages)
	for i := 0; i < totalPages; i++ {
		pageIDs = append(pageIDs, "page_"+uuid.NewString())
	}

	pagesInfo := make([]model.PageStatusInfo, 0, totalPages)
	for _, id := range pageIDs {
		pagesInfo = append(pagesInfo, model.PageStatusInfo{
			PageID:     id,
			Status:     "completed",
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

func (r *InMemoryPPTRepository) InsertPageAfter(taskID, afterPageID string, newPage model.PageRenderResponse) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	canvas, ok := r.canvases[taskID]
	if !ok {
		return ErrTaskNotFound
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
	r.canvases[taskID] = canvas
	if _, ok := r.pages[taskID]; !ok {
		r.pages[taskID] = make(map[string]model.PageRenderResponse)
	}
	r.pages[taskID][newPage.PageID] = newPage
	return nil
}

func (r *InMemoryPPTRepository) InsertPageBefore(taskID, beforePageID string, newPage model.PageRenderResponse) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	canvas, ok := r.canvases[taskID]
	if !ok {
		return ErrTaskNotFound
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
	r.canvases[taskID] = canvas
	if _, ok := r.pages[taskID]; !ok {
		r.pages[taskID] = make(map[string]model.PageRenderResponse)
	}
	r.pages[taskID][newPage.PageID] = newPage
	return nil
}

func (r *InMemoryPPTRepository) DeletePage(taskID, pageID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	canvas, ok := r.canvases[taskID]
	if !ok {
		return ErrTaskNotFound
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
	r.canvases[taskID] = canvas
	delete(r.pages[taskID], pageID)
	return nil
}

func (r *InMemoryPPTRepository) GetTaskIDByPageID(pageID string) (string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for taskID, pages := range r.pages {
		if _, ok := pages[pageID]; ok {
			return taskID, nil
		}
	}
	return "", ErrPageNotFound
}
