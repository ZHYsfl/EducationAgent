// Package testutil 提供测试公共工具：mock 实现、辅助函数。
package testutil

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"kb-service/internal/model"
	"kb-service/internal/store"
	"kb-service/pkg/util"
)

func init() { gin.SetMode(gin.TestMode) }

// ── Mock: MetaStore ───────────────────────────────────────────────────────────

type MockMetaStore struct {
	Colls map[string]*model.KBCollection
	Docs  map[string]*model.KBDocument
	URLs  map[string]bool
}

func NewMockMeta() *MockMetaStore {
	return &MockMetaStore{
		Colls: make(map[string]*model.KBCollection),
		Docs:  make(map[string]*model.KBDocument),
		URLs:  make(map[string]bool),
	}
}

func (m *MockMetaStore) CreateCollection(c *model.KBCollection) error {
	m.Colls[c.CollectionID] = c
	return nil
}
func (m *MockMetaStore) ListCollections(userID string) ([]model.KBCollection, int, error) {
	var list []model.KBCollection
	for _, c := range m.Colls {
		if c.UserID == userID {
			list = append(list, *c)
		}
	}
	return list, len(list), nil
}
func (m *MockMetaStore) GetCollection(collID string) (*model.KBCollection, error) {
	return m.Colls[collID], nil
}
func (m *MockMetaStore) GetDefaultCollection(userID string) (*model.KBCollection, error) {
	for _, c := range m.Colls {
		if c.UserID == userID {
			return c, nil
		}
	}
	return nil, nil
}
func (m *MockMetaStore) IncrDocCount(collID string, now int64) error { return nil }
func (m *MockMetaStore) DecrDocCount(collID string, now int64) error { return nil }
func (m *MockMetaStore) CreateDocument(d *model.KBDocument) error {
	m.Docs[d.DocID] = d
	return nil
}
func (m *MockMetaStore) CreateDocumentFull(d *model.KBDocument, userID, sourceURL string) error {
	m.Docs[d.DocID] = d
	if sourceURL != "" {
		m.URLs[userID+"|"+sourceURL] = true
	}
	return nil
}
func (m *MockMetaStore) GetDocument(docID string) (*model.KBDocument, error) {
	return m.Docs[docID], nil
}
func (m *MockMetaStore) UpdateDocumentStatus(docID, status, errMsg string, chunkCount int) error {
	if d, ok := m.Docs[docID]; ok {
		d.Status = status
		d.ErrorMessage = errMsg
		d.ChunkCount = chunkCount
	}
	return nil
}
func (m *MockMetaStore) DeleteDocument(docID string) (string, error) {
	if d, ok := m.Docs[docID]; ok {
		collID := d.CollectionID
		delete(m.Docs, docID)
		return collID, nil
	}
	return "", nil
}
func (m *MockMetaStore) ListDocumentsByCollection(collID string, page, pageSize int) ([]model.KBDocument, int, error) {
	var list []model.KBDocument
	for _, d := range m.Docs {
		if d.CollectionID == collID {
			list = append(list, *d)
		}
	}
	return list, len(list), nil
}
func (m *MockMetaStore) URLExistsForUser(userID, sourceURL string) (bool, error) {
	return m.URLs[userID+"|"+sourceURL], nil
}
func (m *MockMetaStore) Close() error { return nil }

// ── Mock: VecStore ────────────────────────────────────────────────────────────

type MockVecStore struct{}

func (v *MockVecStore) UpsertChunks(ctx context.Context, chunks []store.ChunkVector) error {
	return nil
}
func (v *MockVecStore) SearchChunks(ctx context.Context, req store.SearchChunksReq) ([]model.RetrievedChunk, error) {
	return nil, nil
}
func (v *MockVecStore) DeleteChunksByDocID(ctx context.Context, docID string) error { return nil }
func (v *MockVecStore) Close() error                                                 { return nil }

// ── Mock: ObjectStorage ───────────────────────────────────────────────────────

type MockOSS struct {
	PutKeys []string
	FailPut bool
}

func (o *MockOSS) Put(ctx context.Context, key string, data io.Reader) (string, error) {
	if o.FailPut {
		return "", bytes.ErrTooLarge
	}
	o.PutKeys = append(o.PutKeys, key)
	return "http://localhost:9200/storage/" + key, nil
}
func (o *MockOSS) Get(ctx context.Context, key string) (io.ReadCloser, error) { return nil, nil }
func (o *MockOSS) Delete(ctx context.Context, key string) error                { return nil }
func (o *MockOSS) Exists(ctx context.Context, key string) (bool, error)        { return false, nil }
func (o *MockOSS) List(ctx context.Context, prefix string) ([]string, error)   { return nil, nil }

// ── HTTP 辅助 ──────────────────────────────────────────────────────────────────

func PostJSON(t *testing.T, r *gin.Engine, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func GetReq(t *testing.T, r *gin.Engine, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func DeleteReq(t *testing.T, r *gin.Engine, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodDelete, path, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func BuildMultipart(t *testing.T, fields map[string]string, fileField, fileName string, fileContent []byte) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	for k, v := range fields {
		_ = w.WriteField(k, v)
	}
	if fileContent != nil && fileField != "" {
		fw, _ := w.CreateFormFile(fileField, fileName)
		_, _ = fw.Write(fileContent)
	}
	w.Close()
	return body, w.FormDataContentType()
}

func UploadReq(t *testing.T, r *gin.Engine, body *bytes.Buffer, contentType string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/kb/upload", body)
	req.Header.Set("Content-Type", contentType)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func DecodeResp(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("响应不是合法 JSON: %s", w.Body.String())
	}
	return m
}

func AssertCode(t *testing.T, resp map[string]any, expected int) {
	t.Helper()
	code := int(resp["code"].(float64))
	if code != expected {
		t.Errorf("期望 code=%d，得到 code=%d，响应=%v", expected, code, resp)
	}
}

func NewUID(prefix string) string {
	return prefix + util.NewID("")
}
