package service

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"auth_memory_service/internal/contract"
	"auth_memory_service/internal/infra/extractor"
	"auth_memory_service/internal/model"
	"auth_memory_service/internal/repository"
	"auth_memory_service/internal/util"
)

type fakeWorkingStore struct {
	items map[string]model.WorkingMemory
}

func (f *fakeWorkingStore) Save(_ context.Context, wm model.WorkingMemory) error {
	if f.items == nil {
		f.items = map[string]model.WorkingMemory{}
	}
	f.items[wm.SessionID] = wm
	return nil
}

func (f *fakeWorkingStore) Get(_ context.Context, sessionID string) (*model.WorkingMemory, error) {
	if f.items == nil {
		return nil, repository.ErrNotFound
	}
	v, ok := f.items[sessionID]
	if !ok {
		return nil, repository.ErrNotFound
	}
	out := v
	return &out, nil
}

func setupMemoryService(t *testing.T) (*MemoryService, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	execSQL(t, db, `CREATE TABLE users (id TEXT PRIMARY KEY, username TEXT UNIQUE, email TEXT UNIQUE, password_hash TEXT, display_name TEXT DEFAULT '', subject TEXT DEFAULT '', school TEXT DEFAULT '', role TEXT DEFAULT 'teacher', created_at INTEGER, updated_at INTEGER);`)
	execSQL(t, db, `CREATE TABLE sessions (id TEXT PRIMARY KEY, user_id TEXT, title TEXT DEFAULT '', status TEXT DEFAULT 'active', created_at INTEGER, updated_at INTEGER);`)
	execSQL(t, db, `CREATE TABLE memory_entries (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, category TEXT NOT NULL, key TEXT NOT NULL, value TEXT NOT NULL, context TEXT DEFAULT 'general', confidence REAL DEFAULT 1.0, source TEXT DEFAULT 'explicit', source_session_id TEXT, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL);`)
	execSQL(t, db, `CREATE UNIQUE INDEX idx_memory_user_key_context ON memory_entries(user_id, key, context);`)
	execSQL(t, db, `INSERT INTO users (id,username,email,password_hash,display_name,subject,school,role,created_at,updated_at) VALUES ('user_u1','u1','u1@example.com','hash','Teacher','Math','School','teacher',1710000000000,1710000000000);`)

	authRepo := repository.NewAuthRepository(db)
	memRepo := repository.NewMemoryRepository(db)
	workingRepo := &fakeWorkingStore{items: map[string]model.WorkingMemory{}}
	svc := NewMemoryService(authRepo, memRepo, workingRepo, extractor.RuleBasedExtractor{})
	return svc, db
}

func execSQL(t *testing.T, db *gorm.DB, sql string, args ...interface{}) {
	t.Helper()
	if err := db.Exec(sql, args...).Error; err != nil {
		t.Fatalf("exec sql: %v", err)
	}
}

func TestWorkingMemorySaveGetAndMissing(t *testing.T) {
	svc, _ := setupMemoryService(t)
	ctx := context.Background()
	err := svc.SaveWorkingMemory(ctx, SaveWorkingMemoryRequest{SessionID: "sess_1", UserID: "user_u1", ConversationSummary: "sum", RecentTopics: []string{"a"}})
	if err != nil {
		t.Fatalf("save working: %v", err)
	}
	wm, err := svc.GetWorkingMemory(ctx, "sess_1")
	if err != nil {
		t.Fatalf("get working: %v", err)
	}
	if wm.UpdatedAt < 1_000_000_000_000 {
		t.Fatalf("updated_at should be ms")
	}
	err = svc.SaveWorkingMemory(ctx, SaveWorkingMemoryRequest{SessionID: "sess_1", UserID: "user_u1", ConversationSummary: "sum2"})
	if err != nil {
		t.Fatalf("save working second: %v", err)
	}
	_, err = svc.GetWorkingMemory(ctx, "sess_missing")
	if err == nil || err.(*ServiceError).Code != contract.CodeResourceNotFound {
		t.Fatalf("missing working should be 40400")
	}
}

func TestExtractUpsertAndSummaryDurable(t *testing.T) {
	svc, db := setupMemoryService(t)
	ctx := context.Background()
	req := MemoryExtractRequest{UserID: "user_u1", SessionID: "sess_1", Messages: []model.ConversationTurn{{Role: "user", Content: "I teach math and prefer interactive style"}}}
	resp, err := svc.Extract(ctx, req)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(resp.ExtractedFacts) == 0 || len(resp.ExtractedPreferences) == 0 {
		t.Fatalf("extract should persist facts and preferences")
	}
	if !strings.HasPrefix(resp.ExtractedFacts[0].ID, "mem_") {
		t.Fatalf("memory id should use mem_ prefix")
	}
	var count int64
	if err := db.Model(&model.MemoryEntry{}).Where("user_id = ? AND category = ?", "user_u1", "summary").Count(&count).Error; err != nil {
		t.Fatalf("query summary count: %v", err)
	}
	if count == 0 {
		t.Fatalf("summary should be durably persisted")
	}

	_, err = svc.Extract(ctx, req)
	if err != nil {
		t.Fatalf("extract second time: %v", err)
	}
	var prefCount int64
	if err := db.Model(&model.MemoryEntry{}).Where("user_id = ? AND category = ? AND key = ?", "user_u1", "preference", "teaching_style").Count(&prefCount).Error; err != nil {
		t.Fatalf("query preference: %v", err)
	}
	if prefCount != 1 {
		t.Fatalf("extract should upsert by user_id/key/context")
	}
}

func TestRecallDecayAndLowConfidence(t *testing.T) {
	svc, db := setupMemoryService(t)
	old := util.NowMilli() - int64(120*24*time.Hour/time.Millisecond)
	execSQL(t, db, `INSERT INTO memory_entries (id,user_id,category,key,value,context,confidence,source,created_at,updated_at) VALUES ('mem_old','user_u1','preference','color_scheme','blue','visual_preferences',0.35,'explicit',?,?);`, old, old)
	execSQL(t, db, `INSERT INTO memory_entries (id,user_id,category,key,value,context,confidence,source,created_at,updated_at) VALUES ('mem_fact','user_u1','fact','subject','math teacher','general',1.0,'explicit',?,?);`, util.NowMilli(), util.NowMilli())
	resp, err := svc.Recall(context.Background(), MemoryRecallRequest{UserID: "user_u1", Query: "color and style", TopK: 10})
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if len(resp.Preferences) == 0 {
		t.Fatalf("expected preference in recall")
	}
	if !strings.HasPrefix(resp.Preferences[0].Value, "[Low Confidence]") {
		t.Fatalf("preference should be low-confidence marked")
	}
}

func TestProfileAggregationAndPartialUpdate(t *testing.T) {
	svc, db := setupMemoryService(t)
	execSQL(t, db, `INSERT INTO memory_entries (id,user_id,category,key,value,context,confidence,source,created_at,updated_at) VALUES ('mem1','user_u1','preference','teaching_style','interactive','general',1.0,'explicit',?,?);`, util.NowMilli(), util.NowMilli())
	execSQL(t, db, `INSERT INTO memory_entries (id,user_id,category,key,value,context,confidence,source,created_at,updated_at) VALUES ('mem2','user_u1','summary','conversation_summary','history text','general',1.0,'inferred',?,?);`, util.NowMilli(), util.NowMilli())
	p, err := svc.GetProfile("user_u1")
	if err != nil {
		t.Fatalf("get profile: %v", err)
	}
	if p.TeachingStyle != "interactive" || p.HistorySummary == "" {
		t.Fatalf("profile aggregation missing expected fields")
	}

	err = svc.UpdateProfile("user_u1", UpdateProfileRequest{DisplayName: "NewName", VisualPreferences: map[string]string{"color_scheme": "teal"}})
	if err != nil {
		t.Fatalf("update profile: %v", err)
	}
	p2, err := svc.GetProfile("user_u1")
	if err != nil {
		t.Fatalf("get profile 2: %v", err)
	}
	if p2.DisplayName != "NewName" {
		t.Fatalf("display_name not updated")
	}
	if p2.TeachingStyle != "interactive" {
		t.Fatalf("partial update should preserve omitted fields")
	}
	if p2.VisualPreferences["color_scheme"] != "teal" {
		t.Fatalf("visual preference update missing")
	}
}
