package asyncjob

import (
	"context"
	"log"

	"memory_service/internal/service"
)

type InProcessDispatcher struct {
	service *service.MemoryService
}

func NewInProcessDispatcher(memoryService *service.MemoryService) *InProcessDispatcher {
	return &InProcessDispatcher{service: memoryService}
}

func (d *InProcessDispatcher) DispatchRecall(_ context.Context, job service.RecallJob) error {
	go func() {
		if err := d.service.ProcessRecall(context.Background(), job); err != nil {
			log.Printf("component=memory_service route_class=canonical operation=recall_async user_id=%s session_id=%s request_id=%s error=%v", job.UserID, job.SessionID, job.RequestID, err)
		}
	}()
	return nil
}

func (d *InProcessDispatcher) DispatchContextPush(_ context.Context, job service.ContextPushJob) error {
	go func() {
		if err := d.service.ProcessContextPush(context.Background(), job); err != nil {
			log.Printf("component=memory_service route_class=canonical operation=context_push_async user_id=%s session_id=%s request_id=%s error=%v", job.UserID, job.SessionID, job.RequestID, err)
		}
	}()
	return nil
}
