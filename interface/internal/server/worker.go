package server

import (
	"context"
	"time"

	"gorm.io/gorm"
)

func (a *App) startFileDeleteWorker(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	go func() {
		for {
			select {
			case <-ticker.C:
				a.pollAndProcessDeleteJobs(ctx)
			case <-ctx.Done():
				ticker.Stop()
				return
			}
		}
	}()
}

func (a *App) pollAndProcessDeleteJobs(ctx context.Context) {
	var jobs []FileDeleteJobModel
	if err := a.db.Where("status IN ? AND retry_count < ?", []string{"pending", "failed"}, a.workerMaxRetry).Order("updated_at ASC").Limit(20).Find(&jobs).Error; err != nil {
		return
	}
	for _, job := range jobs {
		a.processFileDeleteJob(ctx, job)
	}
}

func (a *App) processFileDeleteJob(ctx context.Context, job FileDeleteJobModel) {
	_ = a.db.Model(&FileDeleteJobModel{}).Where("id = ?", job.ID).Updates(map[string]interface{}{"status": "processing", "updated_at": nowMs()}).Error

	err := a.storage.Delete(ctx, job.ObjectKey)
	if err != nil {
		updates := map[string]interface{}{"retry_count": gorm.Expr("retry_count + 1"), "last_error": err.Error(), "updated_at": nowMs(), "status": "failed"}
		_ = a.db.Model(&FileDeleteJobModel{}).Where("id = ?", job.ID).Updates(updates).Error
		return
	}
	_ = a.db.Model(&FileDeleteJobModel{}).Where("id = ?", job.ID).Updates(map[string]interface{}{"status": "done", "last_error": "", "updated_at": nowMs()}).Error
}
