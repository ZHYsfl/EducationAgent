package service

import "zcxppt/internal/model"

type MergeService struct{}

func NewMergeService() *MergeService { return &MergeService{} }

// Three-way merge: base/current/incoming
func (s *MergeService) ThreeWayMerge(base, current, incoming string) model.MergeResult {
	if incoming == "" {
		return model.MergeResult{MergeStatus: "auto_resolved", MergedPyCode: current}
	}
	if base == current {
		return model.MergeResult{MergeStatus: "auto_resolved", MergedPyCode: incoming}
	}
	if current == incoming {
		return model.MergeResult{MergeStatus: "auto_resolved", MergedPyCode: current}
	}
	if base != current && base != incoming && current != incoming {
		return model.MergeResult{MergeStatus: "ask_human", QuestionForUser: "检测到同页并发修改冲突，请确认保留哪版内容。"}
	}
	return model.MergeResult{MergeStatus: "auto_resolved", MergedPyCode: current + "\n" + incoming}
}
