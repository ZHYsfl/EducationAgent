package api

import (
	"unicode/utf8"
)

// InvalidJSONMessage 对齐 FastAPI RequestValidationError 风格的前缀，并对 message 截断（Python 为 800 字节，此处按 rune 截断）。
func InvalidJSONMessage(err error) string {
	const maxRunes = 800
	if err == nil {
		return trimRunes("请求参数无效", maxRunes)
	}
	msg := "请求参数无效：" + err.Error()
	return trimRunes(msg, maxRunes)
}

func trimRunes(s string, max int) string {
	if max <= 0 || s == "" {
		return s
	}
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	if len(r) > max {
		return string(r[:max])
	}
	return s
}
