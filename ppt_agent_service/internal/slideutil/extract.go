package slideutil

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var reQuotedReturn = regexp.MustCompile(`(?s)return\s+"(.*)"\s*$`)

// ExtractHTMLFromPy 从规范形态 py_code（return "..."）中取出 HTML（宽松：无法 Unquote 时退回引号内原始片段）。
func ExtractHTMLFromPy(py string) string {
	py = strings.TrimSpace(py)
	if !strings.Contains(py, "get_slide_markup") {
		return py
	}
	m := reQuotedReturn.FindStringSubmatch(py)
	if len(m) < 2 {
		return py
	}
	s, err := strconv.Unquote(`"` + m[1] + `"`)
	if err != nil {
		return m[1]
	}
	return s
}

// ExtractHTMLFromPyStrict 与 Python extract_slide_html_from_py 一致：规范形态下 return 须为合法引号字符串。
func ExtractHTMLFromPyStrict(py string) (string, error) {
	py = strings.TrimSpace(py)
	if py == "" {
		return "", nil
	}
	if !strings.Contains(py, "get_slide_markup") {
		return py, nil
	}
	m := reQuotedReturn.FindStringSubmatch(py)
	if len(m) < 2 {
		return py, nil
	}
	inner := m[1]
	s, err := strconv.Unquote(`"` + inner + `"`)
	if err != nil {
		return "", fmt.Errorf("%w", err)
	}
	return s, nil
}
