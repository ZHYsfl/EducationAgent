package protocol

import (
	"testing"
)

func TestParser(t *testing.T) {
	parser := NewParser()

	result := parser.Feed("好的，我来帮您创建PPT。@{ppt_init|topic:AI|desc:人工智能介绍}")

	if len(result.Actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(result.Actions))
	}

	action := result.Actions[0]
	if action.Type != "ppt_init" {
		t.Errorf("expected type ppt_init, got %s", action.Type)
	}

	if action.Params["topic"] != "AI" {
		t.Errorf("expected topic AI, got %s", action.Params["topic"])
	}

	if action.Params["desc"] != "人工智能介绍" {
		t.Errorf("expected desc 人工智能介绍, got %s", action.Params["desc"])
	}
}

func TestParserMultiple(t *testing.T) {
	parser := NewParser()

	result := parser.Feed("开始渲染@{render|page:uuid_A}@{render|page:uuid_B}")

	if len(result.Actions) != 2 {
		t.Fatalf("expected 2 actions, got %d", len(result.Actions))
	}
}

func TestParserIncremental(t *testing.T) {
	parser := NewParser()

	// 第一次 feed
	result1 := parser.Feed("好的@{ppt_init|topic:AI}")
	if len(result1.Actions) != 1 {
		t.Fatalf("first feed: expected 1 action, got %d", len(result1.Actions))
	}

	// 第二次 feed - 不应该重复检测到第一个 action
	result2 := parser.Feed("然后@{kb_query|q:test}")
	if len(result2.Actions) != 1 {
		t.Fatalf("second feed: expected 1 new action, got %d", len(result2.Actions))
	}
	if result2.Actions[0].Type != "kb_query" {
		t.Errorf("expected kb_query, got %s", result2.Actions[0].Type)
	}

	// 第三次 feed - 不应该检测到任何 action
	result3 := parser.Feed("完成")
	if len(result3.Actions) != 0 {
		t.Fatalf("third feed: expected 0 actions, got %d", len(result3.Actions))
	}
}
