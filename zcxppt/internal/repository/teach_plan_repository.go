package repository

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"zcxppt/internal/model"
)

// TeachPlanRepository 存储教案内容 JSON，支持三路合并快照。
type TeachPlanRepository interface {
	// InitPlan 初始化教案：首次基于 PyCode 生成教案后调用
	InitPlan(taskID string, planID string, planContentJSON string) error
	// GetPlan 获取当前教案内容 JSON
	GetPlan(taskID string) (string, error)
	// SaveSnapshot 保存当前教案快照（Feedback 每次修改时调用），返回快照时间戳
	SaveSnapshot(taskID string, ts int64) (int64, error)
	// GetSnapshotByTs 获取指定时间戳之前的最新快照
	GetSnapshotByTs(taskID string, ts int64) (string, error)
	// UpdatePlan 更新教案内容 JSON（覆盖写入）
	UpdatePlan(taskID string, planContentJSON string) error
}

// planSnapshot 存储教案的历史快照。
type planSnapshot struct {
	TaskID    string `json:"task_id"`
	Timestamp int64  `json:"timestamp"`
	Content   string `json:"content"` // PlanContent JSON string
}

// InMemoryTeachPlanRepository 是内存实现。
type InMemoryTeachPlanRepository struct {
	mu        sync.RWMutex
	plans     map[string]string           // taskID → PlanContent JSON
	snapshots map[string][]planSnapshot   // taskID → 按时间戳升序的快照切片
}

// NewInMemoryTeachPlanRepository creates a new InMemoryTeachPlanRepository.
func NewInMemoryTeachPlanRepository() *InMemoryTeachPlanRepository {
	return &InMemoryTeachPlanRepository{
		plans:     make(map[string]string),
		snapshots: make(map[string][]planSnapshot),
	}
}

func (r *InMemoryTeachPlanRepository) InitPlan(taskID string, planID string, planContentJSON string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.plans[taskID] = planContentJSON
	// 初始版本也保存为快照
	r.snapshots[taskID] = append(r.snapshots[taskID], planSnapshot{
		TaskID:    taskID,
		Timestamp: time.Now().UnixMilli(),
		Content:   planContentJSON,
	})
	return nil
}

func (r *InMemoryTeachPlanRepository) GetPlan(taskID string) (string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if content, ok := r.plans[taskID]; ok {
		return content, nil
	}
	return "", errors.New("plan not found for task")
}

func (r *InMemoryTeachPlanRepository) SaveSnapshot(taskID string, ts int64) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now().UnixMilli()
	snapshotTs := ts
	if snapshotTs <= 0 {
		snapshotTs = now
	}
	currentContent := ""
	if content, ok := r.plans[taskID]; ok {
		currentContent = content
	}
	if _, ok := r.snapshots[taskID]; !ok {
		r.snapshots[taskID] = make([]planSnapshot, 0)
	}
	r.snapshots[taskID] = append(r.snapshots[taskID], planSnapshot{
		TaskID:    taskID,
		Timestamp: snapshotTs,
		Content:   currentContent,
	})
	return snapshotTs, nil
}

func (r *InMemoryTeachPlanRepository) GetSnapshotByTs(taskID string, ts int64) (string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	snaps, ok := r.snapshots[taskID]
	if !ok || len(snaps) == 0 {
		return "", errors.New("no snapshot found")
	}
	// 找 ts 之前最近的一次快照
	var found planSnapshot
	for _, s := range snaps {
		if s.Timestamp <= ts {
			found = s
		} else {
			break
		}
	}
	if found.TaskID == "" {
		return "", errors.New("no snapshot before specified timestamp")
	}
	return found.Content, nil
}

func (r *InMemoryTeachPlanRepository) UpdatePlan(taskID string, planContentJSON string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.plans[taskID] = planContentJSON
	return nil
}

// MergePlanContent 对两个 PlanContent JSON 进行智能合并（章节级别）。
// 策略：基于 key 对 key 地合并，key 相同则比较内容。
// 返回合并后的 PlanContent JSON。
func MergePlanContent(base, incoming string) (string, error) {
	if incoming == "" {
		return base, nil
	}
	if base == "" {
		return incoming, nil
	}
	if base == incoming {
		return base, nil
	}

	var basePlan, incomingPlan map[string]any
	if err := json.Unmarshal([]byte(base), &basePlan); err != nil {
		return incoming, nil
	}
	if err := json.Unmarshal([]byte(incoming), &incomingPlan); err != nil {
		return incoming, nil
	}

	// 深度合并：遍历 incoming 的 key，若与 base 不同则覆盖
	merged := make(map[string]any)
	for k, bv := range basePlan {
		merged[k] = bv
	}
	for k, iv := range incomingPlan {
		if bv, hasBase := merged[k]; hasBase {
			// 相同 key，比较内容
			if jsonEquals(bv, iv) {
				// 内容相同，保留
				continue
			}
			// 内容不同，以 incoming 为准
			merged[k] = iv
		} else {
			merged[k] = iv
		}
	}
	out, err := json.Marshal(merged)
	if err != nil {
		return incoming, nil
	}
	return string(out), nil
}

func jsonEquals(a, b any) bool {
	aj, _ := json.Marshal(a)
	bj, _ := json.Marshal(b)
	return string(aj) == string(bj)
}

// ExtractPageContentsFromPyCodes 从一组 PyCode 中提取文本内容，构建 []PageContent。
// 用于 Init 阶段从 PPT PyCode 生成教案时的内容输入。
func ExtractPageContentsFromPyCodes(pages []model.PageRenderResponse) []model.PageContent {
	var result []model.PageContent
	for i, page := range pages {
		title, body := extractTextFromPyCode(page.PyCode)
		result = append(result, model.PageContent{
			PageID:    page.PageID,
			PageIndex: i + 1,
			Title:     title,
			BodyText:  body,
			PyCode:    page.PyCode,
		})
	}
	return result
}

// extractTextFromPyCode 从 python-pptx 代码中提取标题和正文文本。
func extractTextFromPyCode(pyCode string) (title string, body string) {
	lines := strings.Split(pyCode, "\n")
	var titleLines []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		// 跳过函数调用行（add_rect/add_oval 等）
		if strings.Contains(trimmed, "add_rect(") ||
			strings.Contains(trimmed, "add_oval(") ||
			strings.Contains(trimmed, "slide.layout") ||
			strings.Contains(trimmed, "prs.save") ||
			strings.Contains(trimmed, "from pptx") ||
			strings.Contains(trimmed, "import ") ||
			strings.Contains(trimmed, "prs =") ||
			strings.Contains(trimmed, "Presentation(") {
			continue
		}
		// 提取带引号的文本内容
		if strings.Contains(trimmed, "\"") || strings.Contains(trimmed, "'") {
			// 简单策略：取第一个引号对之间的文本
			titleLines = append(titleLines, extractQuotedText(trimmed)...)
		}
	}
	if len(titleLines) > 0 {
		title = strings.Join(titleLines[:min(len(titleLines), 3)], " ")
	}
	if len(titleLines) > 3 {
		body = strings.Join(titleLines[3:], " ")
	}
	if title == "" {
		title = "未命名页面"
	}
	return
}

func extractQuotedText(line string) []string {
	var results []string
	rest := line
	for {
		idx1 := -1
		var delim byte
		for i := 0; i < len(rest); i++ {
			if rest[i] == '"' || rest[i] == '\'' {
				idx1 = i
				delim = rest[i]
				break
			}
		}
		if idx1 < 0 {
			break
		}
		rest = rest[idx1+1:]
		idx2 := -1
		for i := 0; i < len(rest); i++ {
			if rest[i] == delim {
				idx2 = i
				break
			}
		}
		if idx2 < 0 {
			break
		}
		text := strings.TrimSpace(rest[:idx2])
		if text != "" && len(text) > 1 {
			results = append(results, text)
		}
		rest = rest[idx2+1:]
	}
	return results
}
