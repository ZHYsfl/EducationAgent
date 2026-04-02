package repository

import (
	"errors"
	"strings"

	"gorm.io/gorm"

	"memory_service/internal/model"
	"memory_service/internal/util"
)

type MemoryRepository struct {
	db *gorm.DB
}

func NewMemoryRepository(db *gorm.DB) *MemoryRepository {
	return &MemoryRepository{db: db}
}

func (r *MemoryRepository) UpsertMemoryEntry(in model.MemoryEntry) (model.MemoryEntry, error) {
	nowMs := util.NowMilli()
	ctx := strings.TrimSpace(in.Context)
	if ctx == "" {
		ctx = "general"
	}
	var existing model.MemoryEntry
	err := r.db.Where("user_id = ? AND key = ? AND context = ?", in.UserID, in.Key, ctx).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		entry := in
		entry.ID = util.NewMemoryID()
		entry.Context = ctx
		entry.CreatedAt = nowMs
		entry.UpdatedAt = nowMs
		if entry.Source == "" {
			entry.Source = "inferred"
		}
		if entry.Confidence == 0 {
			entry.Confidence = 1.0
		}
		if err := r.db.Create(&entry).Error; err != nil {
			return model.MemoryEntry{}, err
		}
		return entry, nil
	}
	if err != nil {
		return model.MemoryEntry{}, err
	}
	updates := map[string]interface{}{
		"category":          in.Category,
		"value":             in.Value,
		"confidence":        in.Confidence,
		"source":            in.Source,
		"source_session_id": in.SourceSessionID,
		"updated_at":        nowMs,
	}
	if updates["source"] == "" {
		updates["source"] = existing.Source
	}
	if in.Confidence == 0 {
		updates["confidence"] = existing.Confidence
	}
	if err := r.db.Model(&model.MemoryEntry{}).Where("id = ?", existing.ID).Updates(updates).Error; err != nil {
		return model.MemoryEntry{}, err
	}
	if err := r.db.Where("id = ?", existing.ID).First(&existing).Error; err != nil {
		return model.MemoryEntry{}, err
	}
	return existing, nil
}

func (r *MemoryRepository) ListMemoryByUser(userID string) ([]model.MemoryEntry, error) {
	var out []model.MemoryEntry
	err := r.db.Where("user_id = ?", userID).Order("updated_at DESC").Find(&out).Error
	return out, err
}

func (r *MemoryRepository) ListMemoryByUserAndCategory(userID, category string) ([]model.MemoryEntry, error) {
	var out []model.MemoryEntry
	err := r.db.Where("user_id = ? AND category = ?", userID, category).Order("updated_at DESC").Find(&out).Error
	return out, err
}

func (r *MemoryRepository) GetLatestSummary(userID string) (string, int64, error) {
	var e model.MemoryEntry
	err := r.db.Where("user_id = ? AND category = ?", userID, "summary").Order("updated_at DESC").First(&e).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", 0, ErrNotFound
	}
	if err != nil {
		return "", 0, err
	}
	return e.Value, e.UpdatedAt, nil
}

func (r *MemoryRepository) UpdateUserProfileFields(userID, displayName, subject string) error {
	updates := map[string]interface{}{}
	if strings.TrimSpace(displayName) != "" {
		updates["display_name"] = strings.TrimSpace(displayName)
	}
	if strings.TrimSpace(subject) != "" {
		updates["subject"] = strings.TrimSpace(subject)
	}
	if len(updates) == 0 {
		return nil
	}
	updates["updated_at"] = util.NowMilli()
	return r.db.Model(&model.User{}).Where("id = ?", userID).Updates(updates).Error
}
