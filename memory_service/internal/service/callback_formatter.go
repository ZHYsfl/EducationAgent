package service

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"memory_service/internal/model"
)

const recallCallbackSummaryBudget = 640

func FormatRecallCallbackSummary(resp MemoryRecallResponse) string {
	sections := make([]string, 0, 3)
	if profile := formatCallbackProfile(resp.ProfileSummary); profile != "" {
		sections = append(sections, "Profile: "+profile)
	}
	if prefs := formatRecallEntries("Preferences", resp.Preferences, 3); prefs != "" {
		sections = append(sections, prefs)
	}
	if facts := formatRecallEntries("Facts", resp.Facts, 3); facts != "" {
		sections = append(sections, facts)
	}
	summary := strings.Join(sections, "\n")
	if summary == "" {
		summary = "No high-signal memory was recalled for this request."
	}
	return trimSummaryBudget(summary, recallCallbackSummaryBudget)
}

func formatCallbackProfile(profile string) string {
	cleaned := compactSummaryText(profile)
	if cleaned == "" {
		return ""
	}
	rawParts := strings.Split(cleaned, "|")
	parts := make([]string, 0, len(rawParts))
	for _, part := range rawParts {
		part = compactSummaryText(part)
		if part == "" {
			continue
		}
		if shouldDropCallbackProfilePart(part) {
			continue
		}
		parts = append(parts, part)
	}
	return compactSummaryText(strings.Join(parts, " | "))
}

func shouldDropCallbackProfilePart(part string) bool {
	lower := strings.ToLower(part)
	if strings.Contains(lower, "history:") || strings.Contains(lower, "current session:") {
		return true
	}
	if strings.Contains(lower, "teaching logic=") || strings.Contains(lower, "reference usage=") {
		return true
	}
	return false
}

func formatRecallEntries(label string, entries []model.MemoryEntry, maxItems int) string {
	if len(entries) == 0 || maxItems <= 0 {
		return ""
	}
	items := make([]string, 0, maxItems)
	for _, entry := range entries {
		if len(items) >= maxItems {
			break
		}
		value := compactSummaryText(entry.Value)
		if value == "" {
			continue
		}
		key := compactSummaryText(entry.Key)
		if key == "" {
			items = append(items, value)
			continue
		}
		items = append(items, fmt.Sprintf("%s=%s", key, value))
	}
	if len(items) == 0 {
		return ""
	}
	return label + ": " + strings.Join(items, "; ")
}

func compactSummaryText(in string) string {
	in = strings.TrimSpace(in)
	if in == "" {
		return ""
	}
	return strings.Join(strings.Fields(in), " ")
}

func trimSummaryBudget(in string, budget int) string {
	if budget <= 0 || utf8.RuneCountInString(in) <= budget {
		return in
	}
	runes := []rune(in)
	if budget <= 1 {
		return string(runes[:budget])
	}
	return strings.TrimSpace(string(runes[:budget-1])) + "…"
}
