package voiceagent

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseAction parses payloads like "update_requirements|topic:math|style:simple".
// It returns the action name and a map of string arguments.
// Arguments without a value are skipped.
func ParseAction(payload string) (string, map[string]string, error) {
	payload = strings.TrimSpace(payload)
	if payload == "" {
		return "", nil, fmt.Errorf("empty action payload")
	}
	parts := strings.Split(payload, "|")
	name := strings.TrimSpace(parts[0])
	if name == "" {
		return "", nil, fmt.Errorf("empty action name")
	}
	args := make(map[string]string)
	for _, p := range parts[1:] {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		kv := strings.SplitN(p, ":", 2)
		key := strings.TrimSpace(kv[0])
		if key == "" {
			continue
		}
		var val string
		if len(kv) == 2 {
			val = strings.TrimSpace(kv[1])
		}
		args[key] = val
	}
	return name, args, nil
}

// ArgsToMap converts string args to map[string]any, attempting to convert
// known numeric fields (e.g. "total_pages") to int.
func ArgsToMap(args map[string]string, intFields ...string) map[string]any {
	intSet := make(map[string]bool, len(intFields))
	for _, f := range intFields {
		intSet[f] = true
	}
	out := make(map[string]any, len(args))
	for k, v := range args {
		if intSet[k] {
			if n, err := strconv.Atoi(v); err == nil {
				out[k] = n
				continue
			}
		}
		out[k] = v
	}
	return out
}
