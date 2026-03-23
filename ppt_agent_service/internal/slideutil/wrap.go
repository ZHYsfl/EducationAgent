package slideutil

import (
	"fmt"
	"strconv"
)

// WrapSlideHTML 生成与 Python wrap_slide_html_as_python 等价的 py_code（return 使用双引号 JSON 风格字面量）。
func WrapSlideHTML(html, pageID string, slideIndex int) string {
	payload := strconv.Quote(html)
	return fmt.Sprintf(
		"# EducationAgent PPT Agent — slide Python source (spec: py_code)\n"+
			"# page_id: %s\n"+
			"# slide_index: %d\n"+
			"\n"+
			"def get_slide_markup() -> str:\n"+
			"    \"\"\"Return slide HTML markup for canvas / renderer.\"\"\"\n"+
			"    return %s\n",
		pageID, slideIndex, payload,
	)
}
