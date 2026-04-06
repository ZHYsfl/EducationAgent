package handler_test

import (
	"testing"

	"kb-service/internal/model"
	"kb-service/pkg/util"
	"kb-service/test/testutil"
)

// ── Query 测试 ────────────────────────────────────────────────────────────────

func TestQuery_MissingUserID(t *testing.T) {
	r := newRouter(testutil.NewMockMeta(), &testutil.MockOSS{})
	resp := testutil.DecodeResp(t, testutil.PostJSON(t, r, "/api/v1/kb/query", map[string]any{
		"query": "什么是线性代数",
	}))
	testutil.AssertCode(t, resp, util.CodeParamError)
}

func TestQuery_MissingQuery(t *testing.T) {
	r := newRouter(testutil.NewMockMeta(), &testutil.MockOSS{})
	resp := testutil.DecodeResp(t, testutil.PostJSON(t, r, "/api/v1/kb/query", map[string]any{
		"user_id": "u1",
	}))
	testutil.AssertCode(t, resp, util.CodeParamError)
}

func TestQuery_Success(t *testing.T) {
	r := newRouter(testutil.NewMockMeta(), &testutil.MockOSS{})
	resp := testutil.DecodeResp(t, testutil.PostJSON(t, r, "/api/v1/kb/query", map[string]any{
		"user_id": "u1",
		"query":   "线性代数基本定理",
		"top_k":   5,
	}))
	testutil.AssertCode(t, resp, util.CodeOK)
	data := resp["data"].(map[string]any)
	if _, ok := data["summary"]; !ok {
		t.Errorf("同步 query 响应缺少 summary 字段")
	}
}

func TestQuery_DefaultTopK(t *testing.T) {
	r := newRouter(testutil.NewMockMeta(), &testutil.MockOSS{})
	resp := testutil.DecodeResp(t, testutil.PostJSON(t, r, "/api/v1/kb/query", map[string]any{
		"user_id": "u1",
		"query": "微积分",
	}))
	testutil.AssertCode(t, resp, util.CodeParamError)
}

func TestQuery_TopKClamped(t *testing.T) {
	// top_k > 20 应被截断为 20，不报错
	r := newRouter(testutil.NewMockMeta(), &testutil.MockOSS{})
	resp := testutil.DecodeResp(t, testutil.PostJSON(t, r, "/api/v1/kb/query", map[string]any{
		"user_id": "u1",
		"query":   "微积分",
		"top_k":   100,
	}))
	testutil.AssertCode(t, resp, util.CodeOK)
}

func TestQuery_EmbedFail(t *testing.T) {
	r := newRouterWithEmbedder(testutil.NewMockMeta(), &testutil.MockOSS{}, &testutil.FailEmbedder{})
	resp := testutil.DecodeResp(t, testutil.PostJSON(t, r, "/api/v1/kb/query", map[string]any{
		"user_id": "u1",
		"query":   "test",
		"top_k":   5,
	}))
	testutil.AssertCode(t, resp, util.CodeDependencyUnavailable)
}

// ── Ingest 测试 ───────────────────────────────────────────────────────────────

func TestIngest_MissingUserID(t *testing.T) {
	// user_id 可选；为空时自动回退为 __public__ 并使用/创建公共默认集合
	r := newRouter(testutil.NewMockMeta(), &testutil.MockOSS{})
	resp := testutil.DecodeResp(t, testutil.PostJSON(t, r, "/api/v1/kb/ingest-from-search", map[string]any{
		"items": []map[string]any{{"url": "http://x", "content": "内容"}},
	}))
	testutil.AssertCode(t, resp, util.CodeOK)
}

func TestIngest_EmptyItems(t *testing.T) {
	r := newRouter(testutil.NewMockMeta(), &testutil.MockOSS{})
	resp := testutil.DecodeResp(t, testutil.PostJSON(t, r, "/api/v1/kb/ingest-from-search", map[string]any{
		"user_id": "u1",
		"items":   []map[string]any{},
	}))
	testutil.AssertCode(t, resp, util.CodeParamError2)
}

func TestIngest_NoCollection(t *testing.T) {
	// user_id 为空时自动回退为 __public__ 并创建公共默认集合
	r := newRouter(testutil.NewMockMeta(), &testutil.MockOSS{})
	resp := testutil.DecodeResp(t, testutil.PostJSON(t, r, "/api/v1/kb/ingest-from-search", map[string]any{
		"items": []map[string]any{{"url": "http://x", "content": "内容", "title": "标题"}},
	}))
	testutil.AssertCode(t, resp, util.CodeOK)
}

func TestIngest_URLDedup(t *testing.T) {
	meta := testutil.NewMockMeta()
	meta.Colls["coll_001"] = &model.KBCollection{CollectionID: "coll_001", UserID: "u1"}
	meta.URLs["u1|http://already.exist"] = true
	r := newRouter(meta, &testutil.MockOSS{})
	resp := testutil.DecodeResp(t, testutil.PostJSON(t, r, "/api/v1/kb/ingest-from-search", map[string]any{
		"user_id":       "u1",
		"collection_id": "coll_001",
		"items": []map[string]any{
			{"url": "http://already.exist", "content": "内容", "title": "标题"},
			{"url": "http://new.url", "content": "新内容", "title": "新标题"},
		},
	}))
	testutil.AssertCode(t, resp, util.CodeOK)
	data := resp["data"].(map[string]any)
	if int(data["ingested"].(float64)) != 1 {
		t.Errorf("期望 ingested=1，得到 %v", data["ingested"])
	}
	if int(data["skipped"].(float64)) != 1 {
		t.Errorf("期望 skipped=1，得到 %v", data["skipped"])
	}
}

func TestIngest_SkipEmptyURLOrContent(t *testing.T) {
	meta := testutil.NewMockMeta()
	meta.Colls["coll_001"] = &model.KBCollection{CollectionID: "coll_001", UserID: "u1"}
	r := newRouter(meta, &testutil.MockOSS{})
	resp := testutil.DecodeResp(t, testutil.PostJSON(t, r, "/api/v1/kb/ingest-from-search", map[string]any{
		"user_id":       "u1",
		"collection_id": "coll_001",
		"items": []map[string]any{
			{"url": "", "content": "内容"},          // url 为空，跳过
			{"url": "http://x", "content": ""},     // content 为空，跳过
			{"url": "http://valid", "content": "ok", "title": "t"}, // 有效
		},
	}))
	testutil.AssertCode(t, resp, util.CodeOK)
	data := resp["data"].(map[string]any)
	if int(data["ingested"].(float64)) != 1 {
		t.Errorf("期望 ingested=1，得到 %v", data["ingested"])
	}
	if int(data["skipped"].(float64)) != 2 {
		t.Errorf("期望 skipped=2，得到 %v", data["skipped"])
	}
}

func TestIngest_Success(t *testing.T) {
	meta := testutil.NewMockMeta()
	meta.Colls["coll_001"] = &model.KBCollection{CollectionID: "coll_001", UserID: "u1"}
	r := newRouter(meta, &testutil.MockOSS{})
	resp := testutil.DecodeResp(t, testutil.PostJSON(t, r, "/api/v1/kb/ingest-from-search", map[string]any{
		"user_id":       "u1",
		"collection_id": "coll_001",
		"items": []map[string]any{
			{"url": "http://a.com", "content": "内容A", "title": "标题A", "source": "a.com"},
			{"url": "http://b.com", "content": "内容B", "title": "标题B", "source": "b.com"},
		},
	}))
	testutil.AssertCode(t, resp, util.CodeOK)
	data := resp["data"].(map[string]any)
	if int(data["ingested"].(float64)) != 2 {
		t.Errorf("期望 ingested=2，得到 %v", data["ingested"])
	}
	if _, ok := data["doc_ids"]; !ok {
		t.Errorf("响应缺少 doc_ids 字段")
	}
}

// ── Parse 测试 ────────────────────────────────────────────────────────────────

func TestParse_MissingFileURL(t *testing.T) {
	r := newRouter(testutil.NewMockMeta(), &testutil.MockOSS{})
	resp := testutil.DecodeResp(t, testutil.PostJSON(t, r, "/api/v1/kb/parse", map[string]any{
		"file_type": "text",
	}))
	testutil.AssertCode(t, resp, util.CodeParamError)
}

func TestParse_MissingFileType(t *testing.T) {
	r := newRouter(testutil.NewMockMeta(), &testutil.MockOSS{})
	resp := testutil.DecodeResp(t, testutil.PostJSON(t, r, "/api/v1/kb/parse", map[string]any{
		"file_url": "http://storage/f.txt",
	}))
	testutil.AssertCode(t, resp, util.CodeParamError)
}

func TestParse_Success(t *testing.T) {
	r := newRouter(testutil.NewMockMeta(), &testutil.MockOSS{})
	resp := testutil.DecodeResp(t, testutil.PostJSON(t, r, "/api/v1/kb/parse", map[string]any{
		"file_url":  "http://storage/f.txt",
		"file_type": "text",
		"doc_id":    "doc_parse_001",
	}))
	testutil.AssertCode(t, resp, util.CodeOK)
	data := resp["data"].(map[string]any)
	if _, ok := data["doc_id"]; !ok {
		t.Errorf("响应缺少 doc_id")
	}
}

func TestParse_AutoDocID(t *testing.T) {
	// 不传 doc_id，应自动生成
	r := newRouter(testutil.NewMockMeta(), &testutil.MockOSS{})
	resp := testutil.DecodeResp(t, testutil.PostJSON(t, r, "/api/v1/kb/parse", map[string]any{
		"file_url":  "http://storage/f.txt",
		"file_type": "text",
	}))
	testutil.AssertCode(t, resp, util.CodeOK)
	data := resp["data"].(map[string]any)
	if data["doc_id"] == "" || data["doc_id"] == nil {
		t.Errorf("doc_id 应自动生成，不能为空")
	}
}
