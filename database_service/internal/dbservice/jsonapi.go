package dbservice

import (
	"encoding/json"
	"net/http"
)

type APIResponse struct {
	Code    int         `json:"code"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data"`
}

func writeJSON(w http.ResponseWriter, statusHTTP int, bizCode int, msg string, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusHTTP)
	_ = json.NewEncoder(w).Encode(APIResponse{Code: bizCode, Message: msg, Data: data})
}

func writeBiz(w http.ResponseWriter, bizCode int, msg string, data interface{}) {
	writeJSON(w, http.StatusOK, bizCode, msg, data)
}

func writeErr(w http.ResponseWriter, bizCode int, msg string) {
	writeBiz(w, bizCode, msg, nil)
}
