package protocol

import (
	"testing"
)

func TestParser(t *testing.T) {
	parser := Parser{}

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
	parser := Parser{}

	result := parser.Feed("开始渲染@{render|page:uuid_A}@{render|page:uuid_B}")

	if len(result.Actions) != 2 {
		t.Fatalf("expected 2 actions, got %d", len(result.Actions))
	}
}
