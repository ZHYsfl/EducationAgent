package service

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"zcxppt/internal/model"
)

// MergeService 实现真正的三路合并（Base / Current / Incoming）。
type MergeService struct{}

// NewMergeService 创建 MergeService 实例。
func NewMergeService() *MergeService { return &MergeService{} }

// ThreeWayMergeInput 包含三路合并所需的全部输入。
// 使用 model.ThreeWayMergeInput 的类型别名以保持接口一致。
type ThreeWayMergeInput = model.ThreeWayMergeInput

// ThreeWayMerge 执行真正的三路合并。
//
// 合并策略：
//  1. Base=Current（从未修改过）→ 直接采纳 Incoming
//  2. Current=Incoming（LLM 未做改动）→ 保持 Current
//  3. Base≠Current≠Incoming（三者均不同）→ 三路 diff 检测冲突
//     - diff(base→current) 与 diff(base→incoming) 无重叠区域 → 自动合并
//     - diff(base→current) 与 diff(base→incoming) 有重叠 → 冲突，进入 ask_human
//  4. Base=Incoming → 说明 Incoming 就是初始版本，但 Current 被其他反馈改了，直接保留 Current
func (s *MergeService) ThreeWayMerge(in ThreeWayMergeInput) model.MergeResult {
	base := strings.TrimSpace(in.BaseCode)
	current := strings.TrimSpace(in.CurrentCode)
	incoming := strings.TrimSpace(in.IncomingCode)

	// Case 1: 无新版本
	if incoming == "" {
		return model.MergeResult{
			PageID:       in.PageID,
			MergeStatus:  "auto_resolved",
			MergedPyCode: current,
			BaseCode:     base,
			CurrentCode:  current,
			IncomingCode: incoming,
		}
	}

	// Case 2: Base=Current，从未修改过，直接采纳 Incoming
	if base == current {
		return model.MergeResult{
			PageID:       in.PageID,
			MergeStatus:  "auto_resolved",
			MergedPyCode: incoming,
			BaseCode:     base,
			CurrentCode:  current,
			IncomingCode: incoming,
		}
	}

	// Case 3: Current=Incoming，LLM 没有实际改动
	if current == incoming {
		return model.MergeResult{
			PageID:       in.PageID,
			MergeStatus:  "auto_resolved",
			MergedPyCode: current,
			BaseCode:     base,
			CurrentCode:  current,
			IncomingCode: incoming,
		}
	}

	// Case 4: Incoming=Base，说明 Incoming 就是初始版本，但 Current 被其他来源修改了
	if incoming == base && current != base {
		return model.MergeResult{
			PageID:       in.PageID,
			MergeStatus:  "auto_resolved",
			MergedPyCode: current,
			BaseCode:     base,
			CurrentCode:  current,
			IncomingCode: incoming,
		}
	}

	// Case 5: 三者均不同，执行三路 diff 检测
	return s.performThreeWayDiff(in)
}

// performThreeWayDiff 对三个版本进行 diff 分析，检测冲突并决定合并策略。
func (s *MergeService) performThreeWayDiff(in ThreeWayMergeInput) model.MergeResult {
	base := in.BaseCode
	current := in.CurrentCode
	incoming := in.IncomingCode

	// 计算三个版本的哈希，快速判断是否真的变化
	_ = hashCode(base)
	currentHash := hashCode(current)
	incomingHash := hashCode(incoming)

	// 计算从 base 到 current 的变更行集合
	currentDiffs := computeLineDiff(base, current)
	// 计算从 base 到 incoming 的变更行集合
	incomingDiffs := computeLineDiff(base, incoming)

	// 检测行级别的变更重叠
	conflictLines := findOverlap(currentDiffs, incomingDiffs)

	if len(conflictLines) == 0 {
		// 无冲突：两个变更修改了不同的行，可以安全自动合并
		merged := mergeCode(base, current, incoming)
		return model.MergeResult{
			PageID:       in.PageID,
			MergeStatus:  "auto_resolved",
			MergedPyCode: merged,
			BaseCode:     base,
			CurrentCode:  current,
			IncomingCode: incoming,
		}
	}

	// 有冲突：分析冲突类型并生成选项
	conflictType := classifyConflict(base, current, incoming, conflictLines)
	question, opts := s.buildConflictQuestion(conflictType, base, current, incoming, conflictLines, currentHash, incomingHash)

	return model.MergeResult{
		PageID:          in.PageID,
		MergeStatus:     "ask_human",
		MergedPyCode:    "", // 等待用户选择
		BaseCode:        base,
		CurrentCode:     current,
		IncomingCode:    incoming,
		ConflictDesc:    conflictType,
		QuestionForUser: question,
		ConflictOpts:    opts,
	}
}

// hashCode 计算字符串的 SHA256 哈希（用于快速比较）。
func hashCode(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:8])
}

// lineDiffEntry 表示一行代码的 diff 信息。
type lineDiffEntry struct {
	lineNum int
	content string
	diffType string // "add", "remove", "modify"
}

// computeLineDiff 计算从 old 到 new 的行级差异，返回变更行的行号集合。
func computeLineDiff(oldStr, newStr string) map[int]lineDiffEntry {
	result := make(map[int]lineDiffEntry)
	oldLines := strings.Split(oldStr, "\n")
	newLines := strings.Split(newStr, "\n")

	// 使用最长公共子序列（LCS）算法找出行级差异
	lcs := computeLCS(oldLines, newLines)

	oldIdx, newIdx, lcsIdx := 0, 0, 0
	oldLineNum, newLineNum := 1, 1

	for oldIdx < len(oldLines) || newIdx < len(newLines) {
		if lcsIdx < len(lcs) && oldIdx < len(oldLines) && newIdx < len(newLines) &&
			oldLines[oldIdx] == lcs[lcsIdx] && newLines[newIdx] == lcs[lcsIdx] {
			oldIdx++
			newIdx++
			lcsIdx++
			oldLineNum++
			newLineNum++
			continue
		}

		// 检测删除（old 有，new 没有）
		if oldIdx < len(oldLines) && (lcsIdx >= len(lcs) || oldLines[oldIdx] != lcs[lcsIdx]) {
			if newIdx < len(newLines) && oldLines[oldIdx] == newLines[newIdx] {
				// 同一行在两侧相同，但位置移动，不算变更
				oldIdx++
				newIdx++
				oldLineNum++
				newLineNum++
			} else {
				result[oldLineNum] = lineDiffEntry{lineNum: oldLineNum, content: oldLines[oldIdx], diffType: "remove"}
				oldIdx++
				oldLineNum++
			}
			continue
		}

		// 检测新增（new 有，old 没有）
		if newIdx < len(newLines) && (lcsIdx >= len(lcs) || newLines[newIdx] != lcs[lcsIdx]) {
			result[newLineNum] = lineDiffEntry{lineNum: newLineNum, content: newLines[newIdx], diffType: "add"}
			newIdx++
			newLineNum++
			continue
		}

		// 位置不匹配时的兜底处理
		if oldIdx < len(oldLines) {
			oldIdx++
			oldLineNum++
		}
		if newIdx < len(newLines) {
			newIdx++
			newLineNum++
		}
	}

	return result
}

// computeLCS 计算两个字符串切片的最长公共子序列。
func computeLCS(a, b []string) []string {
	m, n := len(a), len(b)
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if a[i-1] == b[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else {
				if dp[i-1][j] > dp[i][j-1] {
					dp[i][j] = dp[i-1][j]
				} else {
					dp[i][j] = dp[i][j-1]
				}
			}
		}
	}

	// 回溯
	lcs := make([]string, 0, dp[m][n])
	i, j := m, n
	for i > 0 && j > 0 {
		if a[i-1] == b[j-1] {
			lcs = append([]string{a[i-1]}, lcs...)
			i--
			j--
		} else if dp[i-1][j] > dp[i][j-1] {
			i--
		} else {
			j--
		}
	}
	return lcs
}

// findOverlap 找出两个 diff 结果中重叠的行号（这些行在两侧都被修改了）。
func findOverlap(diffs1, diffs2 map[int]lineDiffEntry) []int {
	var overlaps []int
	// 检查相同行号的变更
	for lineNum := range diffs1 {
		if _, ok := diffs2[lineNum]; ok {
			overlaps = append(overlaps, lineNum)
		}
	}
	// 检查内容相似但行号不同的变更（处理行号偏移的情况）
	if len(overlaps) == 0 {
		overlaps = findContentOverlaps(diffs1, diffs2)
	}
	return overlaps
}

// findContentOverlaps 通过内容相似度检测重叠变更。
func findContentOverlaps(diffs1, diffs2 map[int]lineDiffEntry) []int {
	var overlaps []int
	contentSet1 := make(map[string]bool)
	for _, entry := range diffs1 {
		normalized := normalizeForDiff(entry.content)
		if normalized != "" {
			contentSet1[normalized] = true
		}
	}
	for lineNum, entry := range diffs2 {
		normalized := normalizeForDiff(entry.content)
		if normalized != "" && contentSet1[normalized] {
			overlaps = append(overlaps, lineNum)
		}
	}
	return overlaps
}

// normalizeForDiff 规范化代码行用于 diff 比较（去除空白差异）。
func normalizeForDiff(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\t", " ")
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	return s
}

// classifyConflict 分析冲突类型。
func classifyConflict(base, current, incoming string, conflictLines []int) string {
	if len(conflictLines) == 0 {
		return "no_conflict"
	}

	// 检查是否是结构性变更（添加/删除整块代码）
	currentIsStructural := isStructuralChange(base, current)
	incomingIsStructural := isStructuralChange(base, incoming)

	if currentIsStructural && incomingIsStructural {
		return "structural_conflict"
	}
	if currentIsStructural || incomingIsStructural {
		return "mixed_conflict"
	}

	// 检查是否只是文字内容差异
	currentContentOnly := contentOnlyChange(base, current)
	incomingContentOnly := contentOnlyChange(base, incoming)

	if currentContentOnly && incomingContentOnly {
		return "content_conflict"
	}

	return "general_conflict"
}

func isStructuralChange(base, current string) bool {
	baseLines := strings.Split(strings.TrimSpace(base), "\n")
	currentLines := strings.Split(strings.TrimSpace(current), "\n")
	// 行数变化超过 30% 认为是结构性变更
	if len(baseLines) == 0 {
		return len(currentLines) > 0
	}
	ratio := float64(len(currentLines)) / float64(len(baseLines))
	return ratio < 0.7 || ratio > 1.3
}

func contentOnlyChange(base, current string) bool {
	baseLines := strings.Split(strings.TrimSpace(base), "\n")
	currentLines := strings.Split(strings.TrimSpace(current), "\n")
	return len(baseLines) == len(currentLines)
}

// buildConflictQuestion 根据冲突类型构建问题和选项。
func (s *MergeService) buildConflictQuestion(conflictType, base, current, incoming string, conflictLines []int, currentHash, incomingHash string) (string, []string) {
	switch conflictType {
	case "no_conflict":
		return "", nil

	case "content_conflict":
		// 文字内容冲突：提供三个选项
		return "页面内容检测到冲突：当前版本和新版本对相同内容有不同的修改。请选择保留哪一版：",
			[]string{
				"keep_current:" + currentHash + " 保留当前版本（您的修改）",
				"keep_incoming:" + incomingHash + " 采纳新版本（系统推荐）",
				"keep_base 恢复原始版本",
			}

	case "structural_conflict":
		// 结构性冲突：删除/新增差异较大
		return "页面结构检测到冲突：当前版本和新版本对页面结构有不同的修改（添加或删除了较多内容）。请选择保留哪一版：",
			[]string{
				"keep_current:" + currentHash + " 保留当前版本",
				"keep_incoming:" + incomingHash + " 采纳新版本",
				"keep_base 恢复原始版本",
			}

	case "mixed_conflict":
		return "页面内容检测到混合冲突：当前版本和新版本有部分重叠但整体修改方向不同。请选择保留哪一版：",
			[]string{
				"keep_current:" + currentHash + " 保留当前版本",
				"keep_incoming:" + incomingHash + " 采纳新版本",
				"keep_base 恢复原始版本",
			}

	default:
		return "检测到同页并发修改冲突，请确认保留哪版内容：",
			[]string{
				"keep_current:" + currentHash + " 保留当前版本",
				"keep_incoming:" + incomingHash + " 采纳新版本",
				"keep_base 恢复原始版本",
			}
	}
}

// mergeCode 尝试将两个变更安全合并到一个基础版本上。
// 要求：两个变更不重叠。
func mergeCode(base, current, incoming string) string {
	// 简单策略：以 current 为基础，叠加 incoming 中不在 base 中的新增行
	incomingLines := strings.Split(incoming, "\n")

	// 找出 incoming 比 base 多出来的行（新增内容）
	baseLines := strings.Split(base, "\n")
	addedFromIncoming := findAddedLines(baseLines, incomingLines)

	// 将这些新增内容追加到 current
	if len(addedFromIncoming) == 0 {
		return current
	}

	// 简单追加策略：找到 current 的结尾，在其后追加
	merged := current
	if !strings.HasSuffix(merged, "\n") {
		merged += "\n"
	}
	for _, line := range addedFromIncoming {
		merged += line + "\n"
	}
	return strings.TrimSuffix(merged, "\n")
}

// findAddedLines 找出 new 比 old 多出来的行。
func findAddedLines(oldLines, newLines []string) []string {
	oldSet := make(map[string]bool)
	for _, line := range oldLines {
		oldSet[strings.TrimSpace(line)] = true
	}
	var added []string
	for _, line := range newLines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !oldSet[trimmed] {
			added = append(added, line)
		}
	}
	return added
}

// ResolveUserChoice 根据用户选择的选项解析合并结果。
// choice 格式："keep_current:{hash}" | "keep_incoming:{hash}" | "keep_base"
func (s *MergeService) ResolveUserChoice(choice string, in ThreeWayMergeInput) model.MergeResult {
	choice = strings.TrimSpace(strings.ToLower(choice))

	switch {
	case strings.HasPrefix(choice, "keep_current"):
		return model.MergeResult{
			PageID:       in.PageID,
			MergeStatus:  "auto_resolved",
			MergedPyCode: in.CurrentCode,
			BaseCode:     in.BaseCode,
			CurrentCode:  in.CurrentCode,
			IncomingCode: in.IncomingCode,
		}
	case strings.HasPrefix(choice, "keep_incoming"):
		return model.MergeResult{
			PageID:       in.PageID,
			MergeStatus:  "auto_resolved",
			MergedPyCode: in.IncomingCode,
			BaseCode:     in.BaseCode,
			CurrentCode:  in.CurrentCode,
			IncomingCode: in.IncomingCode,
		}
	case strings.HasPrefix(choice, "keep_base"):
		return model.MergeResult{
			PageID:       in.PageID,
			MergeStatus:  "auto_resolved",
			MergedPyCode: in.BaseCode,
			BaseCode:     in.BaseCode,
			CurrentCode:  in.CurrentCode,
			IncomingCode: in.IncomingCode,
		}
	default:
		// 未知选项，默认保留当前版本
		return model.MergeResult{
			PageID:       in.PageID,
			MergeStatus:  "auto_resolved",
			MergedPyCode: in.CurrentCode,
			BaseCode:     in.BaseCode,
			CurrentCode:  in.CurrentCode,
			IncomingCode: in.IncomingCode,
		}
	}
}
