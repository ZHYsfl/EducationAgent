package api

import (
	"encoding/json"
	"net/http"
)

type Envelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message,omitempty"`
	Data    json.RawMessage `json:"data"`
}

// JSON 写入 {code,message,data}；data 为任意可 JSON 序列化值。
func WriteOK(w http.ResponseWriter, data any) {
	raw, _ := json.Marshal(data)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Code    int             `json:"code"`
		Message string          `json:"message,omitempty"`
		Data    json.RawMessage `json:"data"`
	}{Code: 200, Message: "success", Data: raw})
}

func WriteErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Code    int    `json:"code"`
		Message string `json:"message,omitempty"`
		Data    any    `json:"data"`
	}{Code: code, Message: msg, Data: nil})
}
