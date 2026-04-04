package protocol_test

import (
	"strings"
	"testing"

	"voiceagent/internal/protocol"
)

// ===========================================================================
// ProtocolFilter 基础测试
// ===========================================================================

func TestProtocolFilter_EmptyInput(t *testing.T) {
	var f protocol.ProtocolFilter
	result := f.Feed("")
	if result != "" {
		t.Errorf("expected empty, got %q", result)
	}
}

func TestProtocolFilter_PlainText(t *testing.T) {
	var f protocol.ProtocolFilter
	result := f.Feed("这是一个普通句子。")
	if result != "这是一个普通句子。" {
		t.Errorf("expected plain text, got %q", result)
	}
}

func TestProtocolFilter_SimpleThink(t *testing.T) {
	var f protocol.ProtocolFilter
	result := f.Feed("#{这是思考内容}这是可见内容")
	if result != "这是可见内容" {
		t.Errorf("expected visible content only, got %q", result)
	}
}

func TestProtocolFilter_SimpleAction(t *testing.T) {
	var f protocol.ProtocolFilter
	result := f.Feed("@{update_requirements|topic:数学}这是回复")
	if result != "这是回复" {
		t.Errorf("expected visible content only, got %q", result)
	}
}

func TestProtocolFilter_MixedThinkAndAction(t *testing.T) {
	var f protocol.ProtocolFilter
	result := f.Feed("#{思考}你好@{action|key:value}世界")
	if result != "你好世界" {
		t.Errorf("expected '你好世界', got %q", result)
	}
}

func TestProtocolFilter_NestedMarkers(t *testing.T) {
	var f protocol.ProtocolFilter
	// 嵌套标记：第一个 } 就结束了 think
	result := f.Feed("#{思考@{action}}可见")
	// #{ 开始后，在 action 后的第一个 } 就结束了 think，所以剩下 "}可见"
	// 这是当前实现的行为（简单匹配第一个 }）
	if result != "}可见" {
		t.Errorf("expected '}可见' (first } closes think), got %q", result)
	}
}

func TestProtocolFilter_UnclosedThink(t *testing.T) {
	var f protocol.ProtocolFilter
	// 未闭合的 think 标记应该隐藏后续所有内容
	result := f.Feed("#{未闭合的思考")
	if result != "" {
		t.Errorf("expected empty (unclosed think hides all), got %q", result)
	}

	// 继续输入完成闭合
	result = f.Feed("继续思考}可见")
	if result != "可见" {
		t.Errorf("expected '可见', got %q", result)
	}
}

func TestProtocolFilter_UnclosedAction(t *testing.T) {
	var f protocol.ProtocolFilter
	// 未闭合的 action 标记
	result := f.Feed("@{未闭合的动作")
	if result != "" {
		t.Errorf("expected empty (unclosed action hides all), got %q", result)
	}

	// 完成闭合
	result = f.Feed("继续}可见")
	if result != "可见" {
		t.Errorf("expected '可见', got %q", result)
	}
}

func TestProtocolFilter_PartialMarkerAtEnd(t *testing.T) {
	var f protocol.ProtocolFilter
	// 部分标记在末尾应该保留到下次处理
	result := f.Feed("可见#")
	if result != "可见" {
		t.Errorf("expected '可见', got %q", result)
	}

	// 下次输入完成标记
	result = f.Feed("{思考}内容")
	if result != "内容" {
		t.Errorf("expected '内容', got %q", result)
	}
}

func TestProtocolFilter_PartialAtSign(t *testing.T) {
	var f protocol.ProtocolFilter
	result := f.Feed("可见@")
	if result != "可见" {
		t.Errorf("expected '可见', got %q", result)
	}

	result = f.Feed("{action}内容")
	if result != "内容" {
		t.Errorf("expected '内容', got %q", result)
	}
}

// ===========================================================================
// ProtocolFilter 流式处理测试
// ===========================================================================

func TestProtocolFilter_StreamingTokens(t *testing.T) {
	var f protocol.ProtocolFilter

	// 模拟流式输入
	tokens := []string{
		"你",
		"好",
		"#{思考",
		"内容}",
		"世",
		"界",
		"@{action|k:v}",
		"结束",
	}

	var results []string
	for _, token := range tokens {
		results = append(results, f.Feed(token))
	}

	// 验证结果
	expected := []string{
		"你",
		"好",
		"",
		"",
		"世",
		"界",
		"",
		"结束",
	}

	for i, exp := range expected {
		if results[i] != exp {
			t.Errorf("token %d: expected %q, got %q", i, exp, results[i])
		}
	}
}

func TestProtocolFilter_MultipleThinks(t *testing.T) {
	var f protocol.ProtocolFilter

	input := "开始#{思考1}中间#{思考2}结束"
	result := f.Feed(input)
	if result != "开始中间结束" {
		t.Errorf("expected '开始中间结束', got %q", result)
	}
}

func TestProtocolFilter_MultipleActions(t *testing.T) {
	var f protocol.ProtocolFilter

	input := "开始@{action1|k:v}中间@{action2|k:v}结束"
	result := f.Feed(input)
	if result != "开始中间结束" {
		t.Errorf("expected '开始中间结束', got %q", result)
	}
}

func TestProtocolFilter_ThinkActionInterleaved(t *testing.T) {
	var f protocol.ProtocolFilter

	input := "开始#{思考}@{action}中间#{思考2}@{action2}结束"
	result := f.Feed(input)
	if result != "开始中间结束" {
		t.Errorf("expected '开始中间结束', got %q", result)
	}
}

func TestProtocolFilter_EmptyMarkers(t *testing.T) {
	var f protocol.ProtocolFilter

	// 空思考
	result := f.Feed("内容#{}结束")
	if result != "内容结束" {
		t.Errorf("expected '内容结束', got %q", result)
	}

	// 空动作
	result = f.Feed("内容@{}结束")
	if result != "内容结束" {
		t.Errorf("expected '内容结束', got %q", result)
	}
}

func TestProtocolFilter_SpecialCharactersInContent(t *testing.T) {
	var f protocol.ProtocolFilter

	// 特殊字符在标记内
	input := "可见#{思考}#更多思考}@{a|k:v}}更多"
	result := f.Feed(input)
	// 第一个 } 就结束了 think
	if !strings.Contains(result, "更多思考") {
		t.Logf("result: %q", result)
	}
}

func TestProtocolFilter_UnicodeContent(t *testing.T) {
	var f protocol.ProtocolFilter

	// Unicode 内容
	input := "#{日本語の思考}日本語の返事"
	result := f.Feed(input)
	if result != "日本語の返事" {
		t.Errorf("expected '日本語の返事', got %q", result)
	}
}

func TestProtocolFilter_LongContent(t *testing.T) {
	var f protocol.ProtocolFilter

	// 长内容
	longContent := strings.Repeat("a", 10000)
	input := "#{" + longContent + "}结束"
	result := f.Feed(input)
	if result != "结束" {
		t.Errorf("expected '结束', got %q", result)
	}
}

func TestProtocolFilter_ConsecutiveMarkers(t *testing.T) {
	var f protocol.ProtocolFilter

	// 连续的标记
	input := "#{t1}#{t2}@{a1}@{a2}可见"
	result := f.Feed(input)
	if result != "可见" {
		t.Errorf("expected '可见', got %q", result)
	}
}

func TestProtocolFilter_MarkerAtBoundaries(t *testing.T) {
	var f protocol.ProtocolFilter

	// 标记在开头
	result := f.Feed("#{思考}内容")
	if result != "内容" {
		t.Errorf("expected '内容', got %q", result)
	}

	// 标记在结尾
	result = f.Feed("内容#{思考}")
	if result != "内容" {
		t.Errorf("expected '内容', got %q", result)
	}

	// 只有标记
	result = f.Feed("#{思考}")
	if result != "" {
		t.Errorf("expected empty, got %q", result)
	}
}

// ===========================================================================
// ProtocolFilter 状态保持测试
// ===========================================================================

func TestProtocolFilter_StatePersistence(t *testing.T) {
	var f protocol.ProtocolFilter

	// 第一步：开始思考
	result := f.Feed("开始#{思")
	if result != "开始" {
		t.Errorf("expected '开始', got %q", result)
	}

	// 第二步：继续思考
	result = f.Feed("考内")
	if result != "" {
		t.Errorf("expected empty (in think), got %q", result)
	}

	// 第三步：结束思考
	result = f.Feed("容}可见")
	if result != "可见" {
		t.Errorf("expected '可见', got %q", result)
	}
}

func TestProtocolFilter_StateInAction(t *testing.T) {
	var f protocol.ProtocolFilter

	// 在 action 中保持状态
	result := f.Feed("@{act")
	if result != "" {
		t.Errorf("expected empty, got %q", result)
	}

	result = f.Feed("ion|k:1}可见")
	if result != "可见" {
		t.Errorf("expected '可见', got %q", result)
	}
}

func TestProtocolFilter_MultipleCalls(t *testing.T) {
	var f protocol.ProtocolFilter

	// 多次调用应该累积状态
	result1 := f.Feed("第一")
	result2 := f.Feed("第二")
	result3 := f.Feed("#{思考}")
	result4 := f.Feed("第三")

	if result1 != "第一" {
		t.Errorf("expected '第一', got %q", result1)
	}
	if result2 != "第二" {
		t.Errorf("expected '第二', got %q", result2)
	}
	if result3 != "" {
		t.Errorf("expected empty, got %q", result3)
	}
	if result4 != "第三" {
		t.Errorf("expected '第三', got %q", result4)
	}
}

// ===========================================================================
// ProtocolFilter 复杂场景测试
// ===========================================================================

func TestProtocolFilter_RealWorldExample(t *testing.T) {
	var f protocol.ProtocolFilter

	// 真实场景：思考 + 动作 + 回复
	input := "#{用户想做一个高等数学PPT，需要收集需求}好的@{update_requirements|topic:高等数学}请问目标听众是谁？"
	result := f.Feed(input)

	expected := "好的请问目标听众是谁？"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestProtocolFilter_MultiLineContent(t *testing.T) {
	var f protocol.ProtocolFilter

	input := `#{第一行思考
第二行思考}第一行回复
第二行回复`

	result := f.Feed(input)
	expected := "第一行回复\n第二行回复"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestProtocolFilter_EscapedLikeContent(t *testing.T) {
	var f protocol.ProtocolFilter

	// 内容看起来像标记但实际上不是
	input := "显示#但不隐藏{显示}@但不动作{内容}"
	result := f.Feed(input)

	// 只有当 # 后面紧跟 { 时才算 think 标记
	expected := "显示#但不隐藏{显示}@但不动作{内容}"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestProtocolFilter_BraceBalance(t *testing.T) {
	var f protocol.ProtocolFilter

	// 多个 } 应该只消耗一个
	input := "#{a}}b"
	result := f.Feed(input)
	// #{a} 被隐藏，}b 保留
	if result != "}b" {
		t.Errorf("expected '}b', got %q", result)
	}
}

// ===========================================================================
// 边界情况测试
// ===========================================================================

func TestProtocolFilter_SingleHash(t *testing.T) {
	var f protocol.ProtocolFilter

	result := f.Feed("#")
	if result != "" {
		t.Errorf("expected empty (partial marker), got %q", result)
	}

	// 下次输入不是 {
	result = f.Feed("x")
	if result != "#x" {
		t.Errorf("expected '#x', got %q", result)
	}
}

func TestProtocolFilter_SingleAt(t *testing.T) {
	var f protocol.ProtocolFilter

	result := f.Feed("@")
	if result != "" {
		t.Errorf("expected empty (partial marker), got %q", result)
	}

	result = f.Feed("x")
	if result != "@x" {
		t.Errorf("expected '@x', got %q", result)
	}
}

func TestProtocolFilter_HashThenAt(t *testing.T) {
	var f protocol.ProtocolFilter

	// # 后接 @{
	result := f.Feed("#")
	if result != "" {
		t.Errorf("expected empty, got %q", result)
	}

	result = f.Feed("@{action}")
	// # 被输出了，@{action} 被隐藏
	if result != "#" {
		t.Errorf("expected '#', got %q", result)
	}
}

// ===========================================================================
// 性能测试
// ===========================================================================

func BenchmarkProtocolFilter_ShortText(b *testing.B) {
	input := "#{思考}回复@{action}结束"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var f protocol.ProtocolFilter
		f.Feed(input)
	}
}

func BenchmarkProtocolFilter_LongText(b *testing.B) {
	content := strings.Repeat("a", 1000)
	input := "#{" + content + "}回复@{" + content + "}结束"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var f protocol.ProtocolFilter
		f.Feed(input)
	}
}

func BenchmarkProtocolFilter_Streaming(b *testing.B) {
	tokens := []string{"你", "好", "#{思考}", "世", "界", "@{a}", "!"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var f protocol.ProtocolFilter
		for _, token := range tokens {
			f.Feed(token)
		}
	}
}
