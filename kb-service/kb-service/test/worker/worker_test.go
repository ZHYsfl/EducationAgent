// Package worker_test worker 层黑盒测试
package worker_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"kb-service/internal/model"
	"kb-service/internal/parser"
	"kb-service/internal/store"
	"kb-service/internal/worker"
	"kb-service/test/testutil"
)

// newWorker 创建测试用 worker（队列大小 16，1 个 goroutine）
func newWorker(meta *testutil.MockMetaStore, vec *testutil.MockVecStore, emb parser.Embedder) *worker.IndexWorker {
	return worker.NewIndexWorker(meta, vec, parser.NewSimpleParser(""), emb, 16, 1, 3)
}

// waitStatus 轮询等待文档状态变为非 processing，最多等 3 秒
func waitStatus(t *testing.T, meta *testutil.MockMetaStore, docID string) *model.KBDocument {
	t.Helper()
	for i := 0; i < 30; i++ {
		if d, ok := meta.Docs[docID]; ok && d.Status != "processing" {
			return d
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("等待 doc=%s 索引超时", docID)
	return nil
}

func TestWorker_IndexSuccess(t *testing.T) {
	meta := testutil.NewMockMeta()
	vec := &testutil.MockVecStore{}
	w := newWorker(meta, vec, &parser.MockEmbedder{})

	docID := "doc_w001"
	meta.Docs[docID] = &model.KBDocument{
		DocID: docID, CollectionID: "coll_001", Status: "processing",
	}
	w.Submit(worker.IndexJob{
		DocID:        docID,
		CollectionID: "coll_001",
		UserID:       "u1",
		Content:      fmt.Sprintf("%0500d", 0), // 500字符，足以产生 chunk
		FileType:     "text",
		Title:        "线性代数笔记",
	})

	d := waitStatus(t, meta, docID)
	if d.Status != "indexed" {
		t.Errorf("期望 status=indexed，得到 %s（error: %s）", d.Status, d.ErrorMessage)
	}
}

func TestWorker_EmbedFail(t *testing.T) {
	meta := testutil.NewMockMeta()
	vec := &testutil.MockVecStore{}
	w := newWorker(meta, vec, &testutil.FailEmbedder{})

	docID := "doc_w002"
	meta.Docs[docID] = &model.KBDocument{
		DocID: docID, CollectionID: "coll_001", Status: "processing",
	}
	w.Submit(worker.IndexJob{
		DocID:    docID,
		Content:  fmt.Sprintf("%0500d", 0),
		FileType: "text",
	})

	d := waitStatus(t, meta, docID)
	if d.Status != "failed" {
		t.Errorf("embed 失败时期望 status=failed，得到 %s", d.Status)
	}
	if d.ErrorMessage == "" {
		t.Errorf("失败时 ErrorMessage 不应为空")
	}
}

func TestWorker_UpsertChunksCount(t *testing.T) {
	meta := testutil.NewMockMeta()
	vec := &testutil.MockVecStore{}
	w := newWorker(meta, vec, &parser.MockEmbedder{})

	docID := "doc_w003"
	meta.Docs[docID] = &model.KBDocument{
		DocID: docID, CollectionID: "coll_001", Status: "processing",
	}
	w.Submit(worker.IndexJob{
		DocID:        docID,
		CollectionID: "coll_001",
		UserID:       "u1",
		Content:      fmt.Sprintf("%01200d", 0), // 1200字符，至少产生 1 个 chunk
		FileType:     "text",
	})

	d := waitStatus(t, meta, docID)
	if d.Status != "indexed" {
		t.Fatalf("期望 indexed，得到 %s", d.Status)
	}
	if d.ChunkCount < 1 {
		t.Errorf("期望 chunk_count >= 1，得到 %d", d.ChunkCount)
	}
	if len(vec.Upserted) < 1 {
		t.Errorf("期望 VecStore.UpsertChunks 被调用，实际 upserted=%d", len(vec.Upserted))
	}
}

func TestWorker_WebSnippetOrigin(t *testing.T) {
	meta := testutil.NewMockMeta()
	vec := &testutil.MockVecStore{}
	w := newWorker(meta, vec, &parser.MockEmbedder{})

	docID := "doc_w004"
	meta.Docs[docID] = &model.KBDocument{
		DocID: docID, CollectionID: "coll_001", Status: "processing",
	}
	w.Submit(worker.IndexJob{
		DocID:        docID,
		CollectionID: "coll_001",
		UserID:       "u1",
		Content:      fmt.Sprintf("%0500d", 0),
		FileType:     "web_snippet",
	})

	waitStatus(t, meta, docID)
	for _, cv := range vec.Upserted {
		if cv.Metadata.Origin != "web_search" {
			t.Errorf("web_snippet chunk 的 Origin 应为 web_search，得到 %s", cv.Metadata.Origin)
		}
	}
}

func TestWorker_QueueFull(t *testing.T) {
	// 队列大小为 1，提交 2 个任务，第 2 个应被丢弃（不 panic）
	meta := testutil.NewMockMeta()
	vec := &testutil.MockVecStore{}
	// 用 SleepEmbedder 让 worker 处理第 1 个时阻塞，使队列撑满
	w := worker.NewIndexWorker(meta, vec, parser.NewSimpleParser(""), &testutil.SleepEmbedder{Dur: 200 * time.Millisecond}, 1, 1, 3)

	for i := 0; i < 3; i++ {
		docID := fmt.Sprintf("doc_qf%d", i)
		meta.Docs[docID] = &model.KBDocument{DocID: docID, Status: "processing"}
		w.Submit(worker.IndexJob{
			DocID:   docID,
			Content: fmt.Sprintf("%0500d", i),
			FileType: "text",
		})
	}
	// 不 panic 即为通过
}

func TestWorker_VecStoreUpsertFail(t *testing.T) {
	meta := testutil.NewMockMeta()
	vec := &testutil.MockVecStore{FailUpsert: true}
	w := newWorker(meta, vec, &parser.MockEmbedder{})

	docID := "doc_w005"
	meta.Docs[docID] = &model.KBDocument{
		DocID: docID, CollectionID: "coll_001", Status: "processing",
	}
	w.Submit(worker.IndexJob{
		DocID:    docID,
		Content:  fmt.Sprintf("%0500d", 0),
		FileType: "text",
	})

	d := waitStatus(t, meta, docID)
	if d.Status != "failed" {
		t.Errorf("VecStore upsert 失败时期望 status=failed，得到 %s", d.Status)
	}
}

// ── 确保接口约束 ──────────────────────────────────────────────────────────────

var _ store.VecStore = &testutil.MockVecStore{}
var _ parser.Embedder = &testutil.FailEmbedder{}
var _ parser.Embedder = &testutil.SleepEmbedder{}

// suppress unused import
var _ = context.Background
