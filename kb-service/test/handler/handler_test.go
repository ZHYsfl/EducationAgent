// Package handler_test handler 层黑盒测试（无外部依赖，用 mock 替代所有依赖）
package handler_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"kb-service/internal/handler"
	"kb-service/internal/model"
	"kb-service/internal/parser"
	"kb-service/internal/worker"
	"kb-service/pkg/util"
	"kb-service/test/testutil"
)

// newRouter 构造完整测试路由
func newRouter(meta *testutil.MockMetaStore, oss *testutil.MockOSS) *gin.Engine {
	r := gin.New()
	vec := &testutil.MockVecStore{}
	w := worker.NewIndexWorker(meta, vec, parser.NewSimpleParser(""), &parser.MockEmbedder{}, 4, 1)
	collH := handler.NewCollectionHandler(meta)
	docH := handler.NewDocumentHandler(meta, vec, w, oss)
	r.POST("/api/v1/kb/collections", collH.CreateCollection)
	r.GET("/api/v1/kb/collections", collH.ListCollections)
	r.GET("/api/v1/kb/collections/:collection_id/documents", collH.ListCollectionDocuments)
	r.POST("/api/v1/kb/documents", docH.IndexDocument)
	r.POST("/api/v1/kb/upload", docH.UploadDocument)
	r.GET("/api/v1/kb/documents/:doc_id", docH.GetDocument)
	r.DELETE("/api/v1/kb/documents/:doc_id", docH.DeleteDocument)
	return r
}

// ── Collection 测试 ───────────────────────────────────────────────────────────

func TestCreateCollection_MissingName(t *testing.T) {
	r := newRouter(testutil.NewMockMeta(), &testutil.MockOSS{})
	resp := testutil.DecodeResp(t, testutil.PostJSON(t, r, "/api/v1/kb/collections", map[string]any{
		"user_id": "u1", "subject": "数学",
	}))
	testutil.AssertCode(t, resp, util.CodeParamError)
}

func TestCreateCollection_MissingSubject(t *testing.T) {
	r := newRouter(testutil.NewMockMeta(), &testutil.MockOSS{})
	resp := testutil.DecodeResp(t, testutil.PostJSON(t, r, "/api/v1/kb/collections", map[string]any{
		"user_id": "u1", "name": "数学集合",
	}))
	testutil.AssertCode(t, resp, util.CodeParamError)
}

func TestCreateCollection_MissingUserID(t *testing.T) {
	r := newRouter(testutil.NewMockMeta(), &testutil.MockOSS{})
	resp := testutil.DecodeResp(t, testutil.PostJSON(t, r, "/api/v1/kb/collections", map[string]any{
		"name": "数学集合", "subject": "数学",
	}))
	testutil.AssertCode(t, resp, util.CodeParamError)
}

func TestCreateCollection_Success(t *testing.T) {
	r := newRouter(testutil.NewMockMeta(), &testutil.MockOSS{})
	resp := testutil.DecodeResp(t, testutil.PostJSON(t, r, "/api/v1/kb/collections", map[string]any{
		"user_id": "u1", "name": "数学集合", "subject": "数学",
	}))
	testutil.AssertCode(t, resp, util.CodeOK)
	data := resp["data"].(map[string]any)
	if _, ok := data["collection_id"]; !ok {
		t.Errorf("响应缺少 collection_id")
	}
}

func TestListCollections_MissingUserID(t *testing.T) {
	r := newRouter(testutil.NewMockMeta(), &testutil.MockOSS{})
	resp := testutil.DecodeResp(t, testutil.GetReq(t, r, "/api/v1/kb/collections"))
	testutil.AssertCode(t, resp, util.CodeParamError)
}

func TestListCollections_Success(t *testing.T) {
	meta := testutil.NewMockMeta()
	meta.Colls["coll_001"] = &model.KBCollection{
		CollectionID: "coll_001", UserID: "u1", Name: "测试集合", Subject: "数学",
	}
	r := newRouter(meta, &testutil.MockOSS{})
	resp := testutil.DecodeResp(t, testutil.GetReq(t, r, "/api/v1/kb/collections?user_id=u1"))
	testutil.AssertCode(t, resp, util.CodeOK)
	data := resp["data"].(map[string]any)
	if int(data["total"].(float64)) < 1 {
		t.Errorf("期望至少 1 个集合")
	}
}

// ── Document 测试 ─────────────────────────────────────────────────────────────

func TestIndexDocument_CollectionNotFound(t *testing.T) {
	r := newRouter(testutil.NewMockMeta(), &testutil.MockOSS{})
	resp := testutil.DecodeResp(t, testutil.PostJSON(t, r, "/api/v1/kb/documents", map[string]any{
		"collection_id": "coll_x", "file_id": "f1", "file_url": "http://x", "file_type": "text",
	}))
	testutil.AssertCode(t, resp, util.CodeNotFound)
}

func TestIndexDocument_Success(t *testing.T) {
	meta := testutil.NewMockMeta()
	meta.Colls["coll_001"] = &model.KBCollection{CollectionID: "coll_001", UserID: "u1"}
	r := newRouter(meta, &testutil.MockOSS{})
	resp := testutil.DecodeResp(t, testutil.PostJSON(t, r, "/api/v1/kb/documents", map[string]any{
		"collection_id": "coll_001", "file_id": "f1",
		"file_url": "http://storage/f1.pdf", "file_type": "pdf",
	}))
	testutil.AssertCode(t, resp, util.CodeOK)
	data := resp["data"].(map[string]any)
	if data["status"] != "processing" {
		t.Errorf("status 期望 processing，得到 %v", data["status"])
	}
}

func TestGetDocument_NotFound(t *testing.T) {
	r := newRouter(testutil.NewMockMeta(), &testutil.MockOSS{})
	resp := testutil.DecodeResp(t, testutil.GetReq(t, r, "/api/v1/kb/documents/doc_notexist"))
	testutil.AssertCode(t, resp, util.CodeNotFound)
}

func TestDeleteDocument_NotFound(t *testing.T) {
	r := newRouter(testutil.NewMockMeta(), &testutil.MockOSS{})
	resp := testutil.DecodeResp(t, testutil.DeleteReq(t, r, "/api/v1/kb/documents/doc_notexist"))
	testutil.AssertCode(t, resp, util.CodeNotFound)
}

// ── Upload 测试 ───────────────────────────────────────────────────────────────

func TestUploadDocument_MissingCollectionID(t *testing.T) {
	r := newRouter(testutil.NewMockMeta(), &testutil.MockOSS{})
	body, ct := testutil.BuildMultipart(t, map[string]string{"file_type": "pdf"},
		"file", "test.pdf", []byte("content"))
	resp := testutil.DecodeResp(t, testutil.UploadReq(t, r, body, ct))
	testutil.AssertCode(t, resp, util.CodeParamError)
}

func TestUploadDocument_MissingFileType(t *testing.T) {
	r := newRouter(testutil.NewMockMeta(), &testutil.MockOSS{})
	body, ct := testutil.BuildMultipart(t, map[string]string{"collection_id": "coll_x"},
		"file", "test.pdf", []byte("content"))
	resp := testutil.DecodeResp(t, testutil.UploadReq(t, r, body, ct))
	testutil.AssertCode(t, resp, util.CodeParamError)
}

func TestUploadDocument_MissingFile(t *testing.T) {
	r := newRouter(testutil.NewMockMeta(), &testutil.MockOSS{})
	body, ct := testutil.BuildMultipart(t,
		map[string]string{"collection_id": "coll_x", "file_type": "pdf"},
		"", "", nil)
	resp := testutil.DecodeResp(t, testutil.UploadReq(t, r, body, ct))
	testutil.AssertCode(t, resp, util.CodeParamError)
}

func TestUploadDocument_CollectionNotFound(t *testing.T) {
	r := newRouter(testutil.NewMockMeta(), &testutil.MockOSS{})
	body, ct := testutil.BuildMultipart(t,
		map[string]string{"collection_id": "coll_none", "file_type": "text"},
		"file", "t.txt", []byte("x"))
	resp := testutil.DecodeResp(t, testutil.UploadReq(t, r, body, ct))
	testutil.AssertCode(t, resp, util.CodeNotFound)
}

func TestUploadDocument_OSSFail(t *testing.T) {
	meta := testutil.NewMockMeta()
	meta.Colls["coll_001"] = &model.KBCollection{CollectionID: "coll_001", UserID: "u1"}
	oss := &testutil.MockOSS{FailPut: true}
	r := newRouter(meta, oss)
	body, ct := testutil.BuildMultipart(t,
		map[string]string{"collection_id": "coll_001", "file_type": "text"},
		"file", "t.txt", []byte("content"))
	resp := testutil.DecodeResp(t, testutil.UploadReq(t, r, body, ct))
	testutil.AssertCode(t, resp, util.CodeInternalError)
}

func TestUploadDocument_Success(t *testing.T) {
	meta := testutil.NewMockMeta()
	meta.Colls["coll_001"] = &model.KBCollection{CollectionID: "coll_001", UserID: "u1", Name: "测试"}
	oss := &testutil.MockOSS{}
	r := newRouter(meta, oss)
	body, ct := testutil.BuildMultipart(t,
		map[string]string{"collection_id": "coll_001", "file_type": "text", "title": "测试文档"},
		"file", "test.txt", []byte("文档内容"))
	resp := testutil.DecodeResp(t, testutil.UploadReq(t, r, body, ct))
	testutil.AssertCode(t, resp, util.CodeOK)
	data := resp["data"].(map[string]any)
	if _, ok := data["doc_id"]; !ok {
		t.Errorf("响应缺少 doc_id")
	}
	if _, ok := data["file_url"]; !ok {
		t.Errorf("响应缺少 file_url")
	}
	if data["status"] != "processing" {
		t.Errorf("status 期望 processing，得到 %v", data["status"])
	}
	if len(oss.PutKeys) != 1 {
		t.Errorf("OSS Put 应被调用 1 次，实际 %d 次", len(oss.PutKeys))
	}
}

func TestUploadDocument_OSSKeyFormat(t *testing.T) {
	meta := testutil.NewMockMeta()
	meta.Colls["coll_001"] = &model.KBCollection{CollectionID: "coll_001", UserID: "user_abc"}
	oss := &testutil.MockOSS{}
	r := newRouter(meta, oss)
	body, ct := testutil.BuildMultipart(t,
		map[string]string{"collection_id": "coll_001", "file_type": "pdf"},
		"file", "lecture.pdf", []byte("%PDF"))
	testutil.UploadReq(t, r, body, ct)
	if len(oss.PutKeys) != 1 {
		t.Fatalf("期望 1 个 OSS key，得到 %d", len(oss.PutKeys))
	}
	key := oss.PutKeys[0]
	if !strings.HasPrefix(key, "user_abc/") {
		t.Errorf("key 应以 user_id 开头，得到: %s", key)
	}
	if !strings.HasSuffix(key, ".pdf") {
		t.Errorf("key 应以 .pdf 结尾，得到: %s", key)
	}
}

// ── 辅助（避免 httptest.ResponseRecorder 重复声明）─────────────────────────────
var _ *httptest.ResponseRecorder
