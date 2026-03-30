package integration_test

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"multimodal-teaching-agent/internal/server"
)

// apiResponse is a generic helper matching the common API envelope.
type apiResponse[T any] struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    T      `json:"data"`
}

type fileUploadData struct {
	FileID     string `json:"file_id"`
	Filename   string `json:"filename"`
	FileType   string `json:"file_type"`
	FileSize   int64  `json:"file_size"`
	StorageURL string `json:"storage_url"`
	Purpose    string `json:"purpose"`
}

type sessionCreateData struct {
	SessionID string `json:"session_id"`
	UserID    string `json:"user_id"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	CreatedAt int64  `json:"created_at"`
}

type listSessionsData struct {
	Sessions []struct {
		SessionID string `json:"session_id"`
		UserID    string `json:"user_id"`
		Title     string `json:"title"`
		Status    string `json:"status"`
	} `json:"sessions"`
	Total int64 `json:"total"`
	Page  int   `json:"page"`
}

// TestIntegration_FileAndSessionFlow exercises a happy-path flow across the real
// HTTP stack, database and OSS storage:
//   1) 上传文件
//   2) 根据 file_id 查询文件详情
//   3) 创建会话
//   4) 列出会话并校验新建的会话存在
//   5) 更新会话状态
//
// 运行本测试前，需要本地 PostgreSQL 与 OSS（本地或 MinIO）配置正确，
// DATABASE_URL / OSS_* 环境变量需与生产/文档约定一致。
func TestIntegration_FileAndSessionFlow(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	app, err := server.InitApp()
	if err != nil {
		t.Fatalf("InitApp failed (database / OSS not ready?): %v", err)
	}
	router := server.SetupRouter(app)

	const authHeader = "Bearer user_integration_1"

	// 1. 上传文件
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	fw, err := writer.CreateFormFile("file", "hello.txt")
	if err != nil {
		t.Fatalf("CreateFormFile error: %v", err)
	}
	if _, err = fw.Write([]byte("integration test content")); err != nil {
		t.Fatalf("write form file error: %v", err)
	}
	// 默认 purpose=reference，即可不显式设置
	if err = writer.Close(); err != nil {
		t.Fatalf("multipart writer close error: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/files/upload", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", authHeader)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("upload file HTTP status=%d body=%s", w.Code, w.Body.String())
	}

	var uploadResp apiResponse[fileUploadData]
	if err := json.Unmarshal(w.Body.Bytes(), &uploadResp); err != nil {
		t.Fatalf("unmarshal upload response error: %v, body=%s", err, w.Body.String())
	}
	if uploadResp.Code != 200 {
		t.Fatalf("upload file api code=%d msg=%s", uploadResp.Code, uploadResp.Message)
	}
	if uploadResp.Data.FileID == "" || uploadResp.Data.StorageURL == "" {
		t.Fatalf("upload file missing fields: %+v", uploadResp.Data)
	}
	fileID := uploadResp.Data.FileID

	// 2. 查询文件详情
	req = httptest.NewRequest(http.MethodGet, "/api/v1/files/"+fileID, nil)
	req.Header.Set("Authorization", authHeader)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get file HTTP status=%d body=%s", w.Code, w.Body.String())
	}

	// 为了简单，这里只验证返回的 code 与 file_id 是否一致，不展开所有字段。
	var getResp struct {
		Code int `json:"code"`
		Data struct {
			FileID string `json:"file_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &getResp); err != nil {
		t.Fatalf("unmarshal get file response error: %v, body=%s", err, w.Body.String())
	}
	if getResp.Code != 200 || getResp.Data.FileID != fileID {
		t.Fatalf("unexpected get file response: %+v", getResp)
	}

	// 3. 创建会话
	createBody := []byte(`{"user_id":"user_integration_1","title":"integration-flow"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/sessions", bytes.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", authHeader)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("create session HTTP status=%d body=%s", w.Code, w.Body.String())
	}

	var sessionResp apiResponse[sessionCreateData]
	if err := json.Unmarshal(w.Body.Bytes(), &sessionResp); err != nil {
		t.Fatalf("unmarshal create session response error: %v, body=%s", err, w.Body.String())
	}
	if sessionResp.Code != 200 {
		t.Fatalf("create session api code=%d msg=%s", sessionResp.Code, sessionResp.Message)
	}
	if sessionResp.Data.SessionID == "" || sessionResp.Data.UserID != "user_integration_1" {
		t.Fatalf("unexpected session create data: %+v", sessionResp.Data)
	}
	sessionID := sessionResp.Data.SessionID

	// 4. 列出会话并验证新建会话存在
	req = httptest.NewRequest(http.MethodGet, "/api/v1/sessions?user_id=user_integration_1&page=1&page_size=50", nil)
	req.Header.Set("Authorization", authHeader)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list sessions HTTP status=%d body=%s", w.Code, w.Body.String())
	}

	var listResp apiResponse[listSessionsData]
	if err := json.Unmarshal(w.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("unmarshal list sessions response error: %v, body=%s", err, w.Body.String())
	}
	if listResp.Code != 200 {
		t.Fatalf("list sessions api code=%d msg=%s", listResp.Code, listResp.Message)
	}
	found := false
	for _, s := range listResp.Data.Sessions {
		if s.SessionID == sessionID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("created session %s not found in list", sessionID)
	}

	// 5. 更新会话状态
	updateBody := []byte(`{"status":"completed"}`)
	req = httptest.NewRequest(http.MethodPut, "/api/v1/sessions/"+sessionID, bytes.NewReader(updateBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", authHeader)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update session HTTP status=%d body=%s", w.Code, w.Body.String())
	}

	var updateResp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &updateResp); err != nil {
		t.Fatalf("unmarshal update session response error: %v, body=%s", err, w.Body.String())
	}
	if updateResp.Code != 200 {
		t.Fatalf("update session api code=%d msg=%s", updateResp.Code, updateResp.Message)
	}
}

