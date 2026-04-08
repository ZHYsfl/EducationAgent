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
	SetCurrentViewingPageID(taskID, pageID string) error
	GetPageRender(taskID, pageID string) (model.PageRenderResponse, error)
	UpdatePageCode(taskID, pageID, pyCode, renderURL string) (model.PageRenderResponse, error)
	UpdatePageStatus(taskID, pageID, status, errorMsg string) error
	InsertPageAfter(taskID, afterPageID string, newPage model.PageRenderResponse) error
	InsertPageBefore(taskID, beforePageID string, newPage model.PageRenderResponse) error
	DeletePage(taskID, pageID string) error
	GetTaskIDByPageID(pageID string) (string, error)

	// ── 三路合并快照相关 ────────────────────────────────────────────────────

	// SavePageSnapshot 保存页面快照，返回快照 ID（即时间戳）
	SavePageSnapshot(taskID, pageID string, pyCode string, version int) (int64, error)
	// GetPageSnapshotByTs 获取指定时间戳之前的最新快照（用于按 BaseTimestamp 获取基线版本）
	GetPageSnapshotByTs(taskID, pageID string, ts int64) (model.PageSnapshot, error)
	// GetLatestSnapshot 获取指定页面的最新快照
	GetLatestSnapshot(taskID, pageID string) (model.PageSnapshot, error)
	// GetPageBaseCode 获取页面的初始 BaseCode（首次渲染成功的代码快照）
	GetPageBaseCode(taskID, pageID string) (string, error)
	// SetPageBaseCode 手动设置页面的初始 BaseCode（仅当尚未设置时才写入）
	SetPageBaseCode(taskID, pageID string, baseCode string) error
}

type InMemoryPPTRepository struct {
	mu       sync.RWMutex
	canvases map[string]model.CanvasStatusResponse
	pages    map[string]map[string]model.PageRenderResponse
	// 快照存储：taskID → pageID → 按时间戳升序的快照切片
	snapshots map[string]map[string][]model.PageSnapshot
	// 初始基线快照：taskID → pageID → 首次渲染成功的代码
	baseCodes map[string]map[string]string
}

func NewInMemoryPPTRepository() *InMemoryPPTRepository {
	return &InMemoryPPTRepository{
		canvases:  make(map[string]model.CanvasStatusResponse),
		pages:     make(map[string]map[string]model.PageRenderResponse),
		snapshots: make(map[string]map[string][]model.PageSnapshot),
		baseCodes: make(map[string]map[string]string),
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
	r.canvases[taskID] = canvas

	if _, ok := r.pages[taskID]; !ok {
		r.pages[taskID] = make(map[string]model.PageRenderResponse)
	}
	for _, id := range pageIDs {
		r.pages[taskID][id] = model.PageRenderResponse{
			TaskID:    taskID,
			PageID:    id,
			Status:    "rendering",
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

func (r *InMemoryPPTRepository) SetCurrentViewingPageID(taskID, pageID string) error {
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
	canvas.CurrentViewingPageID = pageID
	r.canvases[taskID] = canvas
	return nil
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

	// ── 三路合并：首次成功渲染时固化 BaseCode ─────────────────────────────
	// 仅当 BaseCode 尚未设置（初始生成阶段）时才写入
	if page.BaseCode == "" && pyCode != "" {
		page.BaseCode = pyCode
		if _, ok := r.baseCodes[taskID]; !ok {
			r.baseCodes[taskID] = make(map[string]string)
		}
		r.baseCodes[taskID][pageID] = pyCode
		// 同时保存一份快照
		now := time.Now().UnixMilli()
		snap := model.PageSnapshot{
			PageID:    pageID,
			TaskID:    taskID,
			PyCode:    pyCode,
			Timestamp: now,
			Version:   page.Version + 1,
		}
		if _, ok := r.snapshots[taskID]; !ok {
			r.snapshots[taskID] = make(map[string][]model.PageSnapshot)
		}
		r.snapshots[taskID][pageID] = append(r.snapshots[taskID][pageID], snap)
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

func (r *InMemoryPPTRepository) UpdatePageStatus(taskID, pageID, status, errorMsg string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	pages, ok := r.pages[taskID]
	if !ok {
		return ErrTaskNotFound
	}
	page, ok := pages[pageID]
	if !ok {
		return ErrPageNotFound
	}
	page.Status = status
	page.ErrorMsg = errorMsg
	page.UpdatedAt = time.Now().UnixMilli()
	pages[pageID] = page

	canvas := r.canvases[taskID]
	for i := range canvas.PagesInfo {
		if canvas.PagesInfo[i].PageID == pageID {
			canvas.PagesInfo[i].Status = status
			canvas.PagesInfo[i].LastUpdate = page.UpdatedAt
		}
	}
	r.canvases[taskID] = canvas
	return nil
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

func (r *InMemoryPPTRepository) SavePageSnapshot(taskID, pageID string, pyCode string, version int) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now().UnixMilli()
	snap := model.PageSnapshot{
		PageID:    pageID,
		TaskID:    taskID,
		PyCode:    pyCode,
		Timestamp: now,
		Version:   version,
	}
	if _, ok := r.snapshots[taskID]; !ok {
		r.snapshots[taskID] = make(map[string][]model.PageSnapshot)
	}
	r.snapshots[taskID][pageID] = append(r.snapshots[taskID][pageID], snap)
	return now, nil
}

func (r *InMemoryPPTRepository) GetPageSnapshotByTs(taskID, pageID string, ts int64) (model.PageSnapshot, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	snaps, ok := r.snapshots[taskID][pageID]
	if !ok || len(snaps) == 0 {
		return model.PageSnapshot{}, errors.New("no snapshot found for page")
	}
	// 找到 ts 之前最近的一次快照
	var found model.PageSnapshot
	for _, s := range snaps {
		if s.Timestamp <= ts {
			found = s
		} else {
			break
		}
	}
	if found.PageID == "" {
		return model.PageSnapshot{}, errors.New("no snapshot before specified timestamp")
	}
	return found, nil
}

func (r *InMemoryPPTRepository) GetLatestSnapshot(taskID, pageID string) (model.PageSnapshot, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	snaps, ok := r.snapshots[taskID][pageID]
	if !ok || len(snaps) == 0 {
		return model.PageSnapshot{}, errors.New("no snapshot found for page")
	}
	return snaps[len(snaps)-1], nil
}

func (r *InMemoryPPTRepository) GetPageBaseCode(taskID, pageID string) (string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if code, ok := r.baseCodes[taskID][pageID]; ok {
		return code, nil
	}
	return "", errors.New("base code not found for page")
}

func (r *InMemoryPPTRepository) SetPageBaseCode(taskID, pageID string, baseCode string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.baseCodes[taskID]; !ok {
		r.baseCodes[taskID] = make(map[string]string)
	}
	// 仅当尚未设置时才写入
	if _, exists := r.baseCodes[taskID][pageID]; !exists {
		r.baseCodes[taskID][pageID] = baseCode
	}
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
