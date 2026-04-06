// embed-server：用 Go 实现的轻量 Embedding HTTP 服务
// 对外暴露 POST /embed 接口，后端调用 Hugging Face Inference API（BAAI/bge-m3）
// 接口约定：POST /embed  {"texts":[...]}  →  {"embeddings":[[...], ...]}
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"
)

// ── 请求/响应结构体 ────────────────────────────────────────────────────────────

type embedRequest struct {
	Texts []string `json:"texts"`
}

type embedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}

// ── Hugging Face Inference API 结构体 ──────────────────────────────────────────
// 参考：https://huggingface.co/docs/api-inference/tasks/feature-extraction

type hfRequest struct {
	Inputs []string `json:"inputs"`
}

// ── 全局配置 ───────────────────────────────────────────────────────────────────

var (
	hfToken  string // HF_TOKEN 环境变量
	hfModel  string // HF_MODEL 环境变量，默认 BAAI/bge-m3
	hfAPIURL string // 完整的 HF Inference API URL
	hfClient = &http.Client{Timeout: 120 * time.Second}
)

func main() {
	// 读取环境变量
	hfToken = getEnv("HF_TOKEN", "")
	hfModel = getEnv("HF_MODEL", "BAAI/bge-m3")
	port := getEnv("EMBED_PORT", "8000")

	if hfToken == "" {
		log.Fatal("[embed-server] HF_TOKEN is required. Set it via environment variable.")
	}

	hfAPIURL = fmt.Sprintf("https://api-inference.huggingface.co/models/%s", hfModel)
	log.Printf("[embed-server] using HF model: %s", hfModel)
	log.Printf("[embed-server] HF API URL: %s", hfAPIURL)

	http.HandleFunc("/embed", handleEmbed)
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	log.Printf("[embed-server] listening on :%s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("[embed-server] server error: %v", err)
	}
}

// handleEmbed 处理 POST /embed 请求
func handleEmbed(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 解析请求体
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("read body: %v", err), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var req embedRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, fmt.Sprintf("unmarshal request: %v", err), http.StatusBadRequest)
		return
	}
	if len(req.Texts) == 0 {
		http.Error(w, "texts is empty", http.StatusBadRequest)
		return
	}

	// 调用 HF Inference API
	embeddings, err := callHFEmbed(req.Texts)
	if err != nil {
		log.Printf("[embed-server] HF API error: %v", err)
		http.Error(w, fmt.Sprintf("embedding failed: %v", err), http.StatusInternalServerError)
		return
	}

	// 返回响应
	resp := embedResponse{Embeddings: embeddings}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("[embed-server] encode response error: %v", err)
	}
}

// callHFEmbed 调用 Hugging Face Inference API 获取向量
func callHFEmbed(texts []string) ([][]float32, error) {
	reqBody, err := json.Marshal(hfRequest{Inputs: texts})
	if err != nil {
		return nil, fmt.Errorf("marshal hf request: %w", err)
	}

	httpReq, err := http.NewRequest(http.MethodPost, hfAPIURL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("new hf request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+hfToken)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := hfClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("hf http do: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read hf response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("hf api error %d: %s", resp.StatusCode, string(respBytes))
	}

	// HF Feature Extraction 返回 [][]float32
	var embeddings [][]float32
	if err := json.Unmarshal(respBytes, &embeddings); err != nil {
		return nil, fmt.Errorf("unmarshal hf response: %w", err)
	}

	return embeddings, nil
}

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
