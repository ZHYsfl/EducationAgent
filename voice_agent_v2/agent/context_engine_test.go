package agent

import "testing"

func TestPickPendingConflictSingleCandidate(t *testing.T) {
	pending := map[string]PendingQuestion{
		"ctx_a": {TaskID: "task_1", PageID: "page_1", QuestionText: "标题是用定义版还是案例版？"},
	}

	cid, ok := pickPendingConflict("用案例版标题", "task_1", "page_1", pending)
	if !ok || cid != "ctx_a" {
		t.Fatalf("expected ctx_a resolved, got ok=%v cid=%q", ok, cid)
	}
}

func TestPickPendingConflictIgnoreTrivialReply(t *testing.T) {
	pending := map[string]PendingQuestion{
		"ctx_a": {TaskID: "task_1", PageID: "page_1", QuestionText: "标题是用定义版还是案例版？"},
	}

	if cid, ok := pickPendingConflict("好的", "task_1", "page_1", pending); ok {
		t.Fatalf("expected unresolved for trivial reply, got cid=%q", cid)
	}
}

func TestPickPendingConflictUseActiveTask(t *testing.T) {
	pending := map[string]PendingQuestion{
		"ctx_a": {TaskID: "task_1", PageID: "page_1", QuestionText: "第一页配图是否需要替换？"},
		"ctx_b": {TaskID: "task_2", PageID: "page_3", QuestionText: "目录页是否需要增加教学目标？"},
	}

	cid, ok := pickPendingConflict("目录页增加教学目标", "task_2", "", pending)
	if !ok || cid != "ctx_b" {
		t.Fatalf("expected ctx_b resolved by active task, got ok=%v cid=%q", ok, cid)
	}
}

func TestPickPendingConflictUseContextIDReference(t *testing.T) {
	pending := map[string]PendingQuestion{
		"ctx_a": {TaskID: "task_1", PageID: "page_1", QuestionText: "第一页配图是否需要替换？"},
		"ctx_b": {TaskID: "task_2", PageID: "page_3", QuestionText: "目录页是否需要增加教学目标？"},
	}

	cid, ok := pickPendingConflict("处理一下 ctx_a 这个冲突", "", "", pending)
	if !ok || cid != "ctx_a" {
		t.Fatalf("expected ctx_a resolved by explicit reference, got ok=%v cid=%q", ok, cid)
	}
}

func TestPickPendingConflictStrongOverlap(t *testing.T) {
	pending := map[string]PendingQuestion{
		"ctx_a": {TaskID: "task_1", PageID: "page_1", QuestionText: "第一页配图是否需要替换？"},
		"ctx_b": {TaskID: "task_2", PageID: "page_3", QuestionText: "请将第一页配色改为蓝绿色渐变并保留简洁科技风格"},
	}

	cid, ok := pickPendingConflict("第一页配色改为蓝绿色渐变，保留简洁科技风格", "", "", pending)
	if !ok || cid != "ctx_b" {
		t.Fatalf("expected ctx_b resolved by overlap, got ok=%v cid=%q", ok, cid)
	}
}

func TestPickPendingConflictAmbiguous(t *testing.T) {
	pending := map[string]PendingQuestion{
		"ctx_a": {TaskID: "task_1", PageID: "page_1", QuestionText: "第一页配图是否需要替换？"},
		"ctx_b": {TaskID: "task_2", PageID: "page_3", QuestionText: "目录页是否需要增加教学目标？"},
	}

	if cid, ok := pickPendingConflict("继续生成下一页", "", "", pending); ok {
		t.Fatalf("expected unresolved for ambiguous reply, got cid=%q", cid)
	}
}
