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

	"memory_service/internal/contract"
	"memory_service/internal/infra/extractor"
	"memory_service/internal/model"
	"memory_service/internal/repository"
	"memory_service/internal/util"
)

type fakeWorkingStore struct {
	items map[string]model.WorkingMemoryRecord
}

func (f *fakeWorkingStore) Save(_ context.Context, wm model.WorkingMemoryRecord) error {
	if f.items == nil {
		f.items = map[string]model.WorkingMemoryRecord{}
	}
	f.items[wm.SessionID] = wm
	return nil
}

func (f *fakeWorkingStore) Get(_ context.Context, sessionID string) (*model.WorkingMemoryRecord, error) {
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

type fakeVoiceAgentClient struct {
	requests []VoicePPTMessageRequest
}

func (f *fakeVoiceAgentClient) SendPPTMessage(_ context.Context, req VoicePPTMessageRequest) error {
	f.requests = append(f.requests, req)
	return nil
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
	execSQL(t, db, `INSERT INTO users (id,username,email,password_hash,display_name,subject,school,role,created_at,updated_at) VALUES ('user_other','other','other@example.com','hash','Other','Physics','School','teacher',1710000000000,1710000000000);`)

	authRepo := repository.NewAuthRepository(db)
	memRepo := repository.NewMemoryRepository(db)
	workingRepo := &fakeWorkingStore{items: map[string]model.WorkingMemoryRecord{}}
	svc := NewMemoryService(authRepo, memRepo, workingRepo, extractor.NewHybridExtractor(extractor.Config{}, nil))
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

func TestWorkingMemoryStoresInternalTaskStateAndProjectsCompatibility(t *testing.T) {
	svc, _ := setupMemoryService(t)
	ctx := context.Background()
	err := svc.SaveWorkingMemory(ctx, SaveWorkingMemoryRequest{
		SessionID:           "sess_internal",
		UserID:              "user_u1",
		ConversationSummary: "use uploaded textbook only",
		ExtractedElements: model.TeachingElements{
			KnowledgePoints: []string{"limits"},
			TeachingGoals:   []string{"explain rate of change"},
			TargetAudience:  "grade 11 students",
			Duration:        "45 minutes",
		},
		RecentTopics: []string{"derivatives"},
	})
	if err != nil {
		t.Fatalf("save working: %v", err)
	}
	store := svc.workingRepo.(*fakeWorkingStore)
	record := store.items["sess_internal"]
	if len(record.TaskState.KnowledgePoints) != 1 || record.TaskState.KnowledgePoints[0] != "limits" {
		t.Fatalf("expected internal task_state knowledge_points, got %#v", record.TaskState)
	}
	if record.TaskState.TargetAudience != "grade 11 students" || record.TaskState.Duration != "45 minutes" {
		t.Fatalf("expected internal task_state projection fields, got %#v", record.TaskState)
	}
	if meta := record.SlotMetadata[model.TaskSlotKnowledgePoints]; meta.Status == "" || meta.Provenance == "" {
		t.Fatalf("expected slot metadata for knowledge points, got %#v", record.SlotMetadata)
	}
	wm, err := svc.GetWorkingMemory(ctx, "sess_internal")
	if err != nil {
		t.Fatalf("get working: %v", err)
	}
	if len(wm.ExtractedElements.KnowledgePoints) != 1 || wm.ExtractedElements.KnowledgePoints[0] != "limits" {
		t.Fatalf("expected outward extracted elements projection, got %#v", wm.ExtractedElements)
	}
	if len(wm.RecentTopics) == 0 || wm.RecentTopics[0] != "derivatives" {
		t.Fatalf("expected recent topics projection, got %#v", wm.RecentTopics)
	}
}

func TestWorkingMemoryRejectsSessionOwnershipMismatch(t *testing.T) {
	svc, db := setupMemoryService(t)
	ctx := context.Background()
	now := util.NowMilli()
	execSQL(t, db, `INSERT INTO sessions (id,user_id,title,status,created_at,updated_at) VALUES ('sess_owned','user_other','owned','active',?,?);`, now, now)

	err := svc.SaveWorkingMemory(ctx, SaveWorkingMemoryRequest{
		SessionID: "sess_owned",
		UserID:    "user_u1",
	})
	if err == nil || err.(*ServiceError).Code != contract.CodeInvalidCredentials {
		t.Fatalf("expected ownership mismatch on save, got %v", err)
	}

	_, err = svc.Recall(ctx, MemoryRecallRequest{
		UserID:    "user_u1",
		SessionID: "sess_owned",
		Query:     "continue this lesson",
		TopK:      3,
	})
	if err == nil || err.(*ServiceError).Code != contract.CodeInvalidCredentials {
		t.Fatalf("expected ownership mismatch on recall, got %v", err)
	}
}

func TestExtractUpsertAndSummaryDurable(t *testing.T) {
	svc, db := setupMemoryService(t)
	ctx := context.Background()
	req := MemoryExtractRequest{
		UserID:    "user_u1",
		SessionID: "sess_1",
		Messages: []model.ConversationTurn{
			{Role: "user", Content: "I teach math at a high school and I prefer a clean minimalist PPT style."},
			{Role: "user", Content: "The knowledge points include limits, derivatives, and tangent lines."},
			{Role: "user", Content: "The teaching goals are helping students understand derivative intuition and apply tangent line ideas."},
			{Role: "user", Content: "This lesson is for grade 11 students and should fit into 45 minutes."},
		},
	}
	resp, err := svc.Extract(ctx, req)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(resp.ExtractedFacts) == 0 || len(resp.ExtractedPreferences) == 0 {
		t.Fatalf("extract should persist durable facts and preferences")
	}
	if !strings.HasPrefix(resp.ExtractedFacts[0].ID, "mem_") {
		t.Fatalf("memory id should use mem_ prefix")
	}
	wm, err := svc.GetWorkingMemory(ctx, "sess_1")
	if err != nil {
		t.Fatalf("get working memory: %v", err)
	}
	if len(wm.ExtractedElements.KnowledgePoints) == 0 || len(wm.ExtractedElements.TeachingGoals) == 0 {
		t.Fatalf("teaching elements should be written to working memory")
	}
	if wm.ExtractedElements.TargetAudience == "" || wm.ExtractedElements.Duration == "" {
		t.Fatalf("target audience and duration should be normalized into working memory")
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
	if err := db.Model(&model.MemoryEntry{}).Where("user_id = ? AND category = ? AND key = ?", "user_u1", "preference", "output_style").Count(&prefCount).Error; err != nil {
		t.Fatalf("query preference: %v", err)
	}
	if prefCount != 1 {
		t.Fatalf("extract should upsert by user_id/key/context")
	}
	var lessonTopicCount int64
	if err := db.Model(&model.MemoryEntry{}).Where("user_id = ? AND key IN ?", "user_u1", []string{"knowledge_points", "teaching_goals", "target_audience", "duration"}).Count(&lessonTopicCount).Error; err != nil {
		t.Fatalf("query lesson-specific entries: %v", err)
	}
	if lessonTopicCount != 0 {
		t.Fatalf("session-specific lesson requirements should not be persisted as long-term memory entries")
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

func TestExtractMergesTeachingElementsWithoutDroppingExistingWorkingMemory(t *testing.T) {
	svc, _ := setupMemoryService(t)
	ctx := context.Background()
	err := svc.SaveWorkingMemory(ctx, SaveWorkingMemoryRequest{
		SessionID: "sess_merge",
		UserID:    "user_u1",
		ExtractedElements: model.TeachingElements{
			KnowledgePoints: []string{"existing point"},
			OutputStyle:     "existing style",
		},
		RecentTopics: []string{"previous topic"},
	})
	if err != nil {
		t.Fatalf("seed working memory: %v", err)
	}

	_, err = svc.Extract(ctx, MemoryExtractRequest{
		UserID:    "user_u1",
		SessionID: "sess_merge",
		Messages: []model.ConversationTurn{
			{Role: "user", Content: "The teaching goals are helping students compare derivatives with slope ideas."},
			{Role: "user", Content: "This lesson is for grade 10 students."},
		},
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	wm, err := svc.GetWorkingMemory(ctx, "sess_merge")
	if err != nil {
		t.Fatalf("get working memory: %v", err)
	}
	if len(wm.ExtractedElements.KnowledgePoints) != 1 || wm.ExtractedElements.KnowledgePoints[0] != "existing point" {
		t.Fatalf("existing knowledge points should be preserved when no better replacement is extracted")
	}
	if len(wm.ExtractedElements.TeachingGoals) == 0 {
		t.Fatalf("new teaching goals should be merged into working memory")
	}
	if wm.ExtractedElements.TargetAudience == "" {
		t.Fatalf("new target audience should be merged into working memory")
	}
	if wm.ExtractedElements.OutputStyle != "existing style" {
		t.Fatalf("existing output style should be preserved when the new extract call has no confident style")
	}
}

func TestExtractUpdatesWorkingMemoryWithStrongerNormalizedValuesAndDedupesLists(t *testing.T) {
	svc, _ := setupMemoryService(t)
	ctx := context.Background()
	err := svc.SaveWorkingMemory(ctx, SaveWorkingMemoryRequest{
		SessionID: "sess_upgrade",
		UserID:    "user_u1",
		ExtractedElements: model.TeachingElements{
			KnowledgePoints: []string{"limits", "limits"},
			TargetAudience:  "students",
			Duration:        "30 minutes",
		},
	})
	if err != nil {
		t.Fatalf("seed working memory: %v", err)
	}

	_, err = svc.Extract(ctx, MemoryExtractRequest{
		UserID:    "user_u1",
		SessionID: "sess_upgrade",
		Messages: []model.ConversationTurn{
			{Role: "user", Content: "The knowledge points include limits, derivatives."},
			{Role: "user", Content: "This lesson is for first-year calculus students and should fit into 45 minutes."},
		},
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	wm, err := svc.GetWorkingMemory(ctx, "sess_upgrade")
	if err != nil {
		t.Fatalf("get working memory: %v", err)
	}
	if wm.ExtractedElements.TargetAudience != "This lesson is for first-year calculus students and should fit into 45 minutes" {
		t.Fatalf("expected stronger target audience value to overwrite weaker prior value, got %q", wm.ExtractedElements.TargetAudience)
	}
	if wm.ExtractedElements.Duration != "45 minutes" {
		t.Fatalf("expected duration update, got %q", wm.ExtractedElements.Duration)
	}
	countLimits := 0
	hasDerivatives := false
	for _, point := range wm.ExtractedElements.KnowledgePoints {
		if strings.EqualFold(point, "limits") {
			countLimits++
		}
		if strings.EqualFold(point, "derivatives") {
			hasDerivatives = true
		}
	}
	if countLimits != 1 || !hasDerivatives {
		t.Fatalf("expected deduped knowledge points with preserved new coverage, got %#v", wm.ExtractedElements.KnowledgePoints)
	}
}

func TestExtractKeepsUnsupportedPlanningFieldsInSummaryOnly(t *testing.T) {
	svc, db := setupMemoryService(t)
	ctx := context.Background()
	resp, err := svc.Extract(ctx, MemoryExtractRequest{
		UserID:    "user_u1",
		SessionID: "sess_summary",
		Messages: []model.ConversationTurn{
			{Role: "user", Content: "First teach force concepts, then compare examples, and finally use a short quiz."},
			{Role: "user", Content: "Use the uploaded textbook PDF only for diagrams and mimic the layout of my previous PPT."},
			{Role: "user", Content: "Add a quick interaction with an animation if possible."},
		},
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if !strings.Contains(resp.ConversationSummary, "teaching logic") {
		t.Fatalf("expected teaching logic note in summary")
	}
	if !strings.Contains(resp.ConversationSummary, "reference usage") {
		t.Fatalf("expected reference usage note in summary")
	}
	if !strings.Contains(resp.ConversationSummary, "interaction") {
		t.Fatalf("expected interaction note in summary")
	}
	wm, err := svc.GetWorkingMemory(ctx, "sess_summary")
	if err != nil {
		t.Fatalf("get working memory: %v", err)
	}
	if len(wm.ExtractedElements.KnowledgePoints) != 0 || len(wm.ExtractedElements.TeachingGoals) != 0 || wm.ExtractedElements.OutputStyle != "" {
		t.Fatalf("unsupported planning fields should stay out of teaching elements: %#v", wm.ExtractedElements)
	}
	var pollutedCount int64
	if err := db.Model(&model.MemoryEntry{}).Where("user_id = ? AND key IN ?", "user_u1", []string{"teaching_logic", "interaction_design", "reference_file_usage"}).Count(&pollutedCount).Error; err != nil {
		t.Fatalf("query planning-field pollution: %v", err)
	}
	if pollutedCount != 0 {
		t.Fatalf("unsupported planning fields should not be persisted as long-term memory")
	}
	record := svc.workingRepo.(*fakeWorkingStore).items["sess_summary"]
	if record.TaskState.TeachingLogic == "" {
		t.Fatalf("expected teaching logic in internal task_state")
	}
	if len(record.TaskState.ReferenceMaterialUsage) == 0 {
		t.Fatalf("expected reference usage in internal task_state")
	}
}

func TestExtractChineseDialoguePopulatesWorkingMemoryWithoutDurableTaskLocalStyle(t *testing.T) {
	svc, db := setupMemoryService(t)
	ctx := context.Background()
	resp, err := svc.Extract(ctx, MemoryExtractRequest{
		UserID:    "user_u1",
		SessionID: "sess_cn",
		Messages: []model.ConversationTurn{
			{Role: "user", Content: "这节课讲牛顿第一定律，知识点包括惯性、受力分析。"},
			{Role: "user", Content: "面向高一学生，时长45分钟。"},
			{Role: "user", Content: "这次课件用深蓝简洁风格，只用教材里的图。"},
		},
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	wm, err := svc.GetWorkingMemory(ctx, "sess_cn")
	if err != nil {
		t.Fatalf("get working: %v", err)
	}
	if len(wm.ExtractedElements.KnowledgePoints) == 0 {
		t.Fatalf("expected chinese knowledge points in working memory")
	}
	if wm.ExtractedElements.Duration != "45分钟" {
		t.Fatalf("expected chinese duration normalization, got %q", wm.ExtractedElements.Duration)
	}
	if wm.ExtractedElements.OutputStyle == "" {
		t.Fatalf("expected task-local chinese output style in working memory")
	}
	if !strings.Contains(resp.ConversationSummary, "reference usage") {
		t.Fatalf("expected chinese reference usage note in projected summary, got %q", resp.ConversationSummary)
	}
	var durableStyleCount int64
	if err := db.Model(&model.MemoryEntry{}).Where("user_id = ? AND category = ? AND key = ?", "user_u1", "preference", "output_style").Count(&durableStyleCount).Error; err != nil {
		t.Fatalf("query style preferences: %v", err)
	}
	if durableStyleCount != 0 {
		t.Fatalf("task-local chinese style instruction should not persist durably")
	}
}

func TestExtractChineseMixedDialoguePreventsFieldPollutionRegression(t *testing.T) {
	svc, _ := setupMemoryService(t)
	ctx := context.Background()
	_, err := svc.Extract(ctx, MemoryExtractRequest{
		UserID:    "user_u1",
		SessionID: "sess_cn_mixed",
		Messages: []model.ConversationTurn{
			{Role: "user", Content: "我平时偏好简洁正式的课件风格，通常先讲概念再做例题。"},
			{Role: "user", Content: "这次是初二勾股定理课件，40分钟，重点讲证明和基础应用。"},
		},
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	wm, err := svc.GetWorkingMemory(ctx, "sess_cn_mixed")
	if err != nil {
		t.Fatalf("get working memory: %v", err)
	}
	if wm.ExtractedElements.OutputStyle == "" {
		t.Fatalf("expected output style to be preserved")
	}
	if strings.Contains(wm.ExtractedElements.OutputStyle, "先讲概念再做例题") {
		t.Fatalf("output_style should not contain sequencing clause: %q", wm.ExtractedElements.OutputStyle)
	}
	for _, point := range wm.ExtractedElements.KnowledgePoints {
		if strings.Contains(point, "先讲") || strings.Contains(point, "再做") {
			t.Fatalf("knowledge points polluted by sequencing fragments: %#v", wm.ExtractedElements.KnowledgePoints)
		}
	}
	for _, diff := range wm.ExtractedElements.KeyDifficulties {
		if strings.Contains(diff, "讲证明和基础应用") {
			t.Fatalf("key difficulties polluted by action-sequence fragment: %#v", wm.ExtractedElements.KeyDifficulties)
		}
	}
}

func TestExtractChineseStandingPreferencePromotesDurableVisualPreference(t *testing.T) {
	svc, db := setupMemoryService(t)
	ctx := context.Background()
	resp, err := svc.Extract(ctx, MemoryExtractRequest{
		UserID:    "user_u1",
		SessionID: "sess_cn_pref",
		Messages: []model.ConversationTurn{
			{Role: "user", Content: "我平时喜欢简洁的蓝色课件风格。"},
			{Role: "user", Content: "这节课讲导数。"},
		},
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(resp.ExtractedPreferences) == 0 {
		t.Fatalf("expected durable chinese standing preference")
	}
	var durableStyleCount int64
	if err := db.Model(&model.MemoryEntry{}).Where("user_id = ? AND category = ? AND key = ?", "user_u1", "preference", "output_style").Count(&durableStyleCount).Error; err != nil {
		t.Fatalf("query style preferences: %v", err)
	}
	if durableStyleCount != 1 {
		t.Fatalf("expected one durable chinese output_style preference, got %d", durableStyleCount)
	}
}

func TestExtractPreventsLongTermPollutionFromOneOffLessonRequirements(t *testing.T) {
	svc, db := setupMemoryService(t)
	ctx := context.Background()
	_, err := svc.Extract(ctx, MemoryExtractRequest{
		UserID:    "user_u1",
		SessionID: "sess_policy",
		Messages: []model.ConversationTurn{
			{Role: "user", Content: "For this lesson only, make a comic-style deck on Newton's laws for grade 8 students."},
			{Role: "user", Content: "The duration is 40 minutes and the teaching goals are comparing force and motion."},
			{Role: "user", Content: "Use the uploaded PDF just for reference examples and keep the teaching logic as concept first, examples second."},
			{Role: "user", Content: "Across all my lessons I prefer clean minimalist slides."},
			{Role: "user", Content: "I teach physics in high school."},
		},
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	var durableCount int64
	if err := db.Model(&model.MemoryEntry{}).Where("user_id = ? AND key IN ?", "user_u1", []string{"subject", "school_context", "output_style"}).Count(&durableCount).Error; err != nil {
		t.Fatalf("query durable entries: %v", err)
	}
	if durableCount == 0 {
		t.Fatalf("expected durable facts/preferences to persist")
	}

	var lessonOnlyCount int64
	if err := db.Model(&model.MemoryEntry{}).Where("user_id = ? AND key IN ?", "user_u1", []string{
		"topic", "target_audience", "duration", "teaching_logic", "reference_file_usage",
	}).Count(&lessonOnlyCount).Error; err != nil {
		t.Fatalf("query one-off lesson entries: %v", err)
	}
	if lessonOnlyCount != 0 {
		t.Fatalf("one-off lesson requirements should not pollute long-term memory")
	}

	var oneOffStyleCount int64
	if err := db.Model(&model.MemoryEntry{}).Where("user_id = ? AND category = ? AND value LIKE ?", "user_u1", "preference", "%comic-style%").Count(&oneOffStyleCount).Error; err != nil {
		t.Fatalf("query one-off style: %v", err)
	}
	if oneOffStyleCount != 0 {
		t.Fatalf("one-off output style should not persist as a durable preference")
	}
}

func TestRecallUsesHybridCompactBudgetAndPreferenceBias(t *testing.T) {
	svc, db := setupMemoryService(t)
	now := util.NowMilli()
	execSQL(t, db, `INSERT INTO memory_entries (id,user_id,category,key,value,context,confidence,source,created_at,updated_at) VALUES ('mem_fact_a','user_u1','fact','subject','mathematics teacher','general',1.0,'explicit',?,?);`, now, now)
	execSQL(t, db, `INSERT INTO memory_entries (id,user_id,category,key,value,context,confidence,source,created_at,updated_at) VALUES ('mem_fact_b','user_u1','fact','school_context','high school class','general',1.0,'explicit',?,?);`, now, now)
	execSQL(t, db, `INSERT INTO memory_entries (id,user_id,category,key,value,context,confidence,source,created_at,updated_at) VALUES ('mem_pref_a','user_u1','preference','output_style','minimal blue style','visual_preferences',0.9,'explicit',?,?);`, now, now)
	execSQL(t, db, `INSERT INTO memory_entries (id,user_id,category,key,value,context,confidence,source,created_at,updated_at) VALUES ('mem_pref_b','user_u1','preference','color_scheme','blue','visual_preferences',0.9,'explicit',?,?);`, now, now)
	execSQL(t, db, `INSERT INTO memory_entries (id,user_id,category,key,value,context,confidence,source,created_at,updated_at) VALUES ('mem_pref_c','user_u1','preference','font_style','clean sans-serif style','visual_preferences',0.9,'explicit',?,?);`, now, now)

	resp, err := svc.Recall(context.Background(), MemoryRecallRequest{
		UserID: "user_u1",
		Query:  "what style and color do I prefer",
		TopK:   3,
	})
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	total := len(resp.Facts) + len(resp.Preferences)
	if total > 3 {
		t.Fatalf("hybrid budget should cap total returned entries by top_k, got %d", total)
	}
	if len(resp.Preferences) < len(resp.Facts) {
		t.Fatalf("preference-focused query should bias compact budget toward preferences")
	}
}

func TestRecallSessionSignalsAndSessionSourceBoostFactRanking(t *testing.T) {
	svc, db := setupMemoryService(t)
	ctx := context.Background()
	now := util.NowMilli()
	sessID := "sess_continue"

	if err := svc.SaveWorkingMemory(ctx, SaveWorkingMemoryRequest{
		SessionID: sessID,
		UserID:    "user_u1",
		ExtractedElements: model.TeachingElements{
			KnowledgePoints: []string{"derivatives"},
			TargetAudience:  "grade 11 students",
			Duration:        "45 minutes",
		},
		RecentTopics: []string{"limits"},
	}); err != nil {
		t.Fatalf("save working memory: %v", err)
	}

	execSQL(t, db, `INSERT INTO memory_entries (id,user_id,category,key,value,context,confidence,source,source_session_id,created_at,updated_at) VALUES ('mem_fact_match','user_u1','fact','lesson_focus','derivatives and tangent line intuition','general',1.0,'inferred',?,?,?);`, sessID, now, now)
	execSQL(t, db, `INSERT INTO memory_entries (id,user_id,category,key,value,context,confidence,source,created_at,updated_at) VALUES ('mem_fact_other','user_u1','fact','lesson_topic','geometry proof structure','general',1.0,'inferred',?,?);`, now, now)

	resp, err := svc.Recall(ctx, MemoryRecallRequest{
		UserID:    "user_u1",
		SessionID: sessID,
		Query:     "continue this lesson",
		TopK:      2,
	})
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if len(resp.Facts) == 0 {
		t.Fatalf("expected ranked facts")
	}
	if resp.Facts[0].ID != "mem_fact_match" {
		t.Fatalf("session-aligned fact should rank first, got %s", resp.Facts[0].ID)
	}
}

func TestRecallProfileSummaryIsDurableFirstAndSessionConditional(t *testing.T) {
	svc, db := setupMemoryService(t)
	ctx := context.Background()
	now := util.NowMilli()
	sessID := "sess_summary_mode"

	execSQL(t, db, `INSERT INTO memory_entries (id,user_id,category,key,value,context,confidence,source,created_at,updated_at) VALUES ('mem_style','user_u1','preference','teaching_style','interactive','general',1.0,'explicit',?,?);`, now, now)
	execSQL(t, db, `INSERT INTO memory_entries (id,user_id,category,key,value,context,confidence,source,created_at,updated_at) VALUES ('mem_hist','user_u1','summary','conversation_summary','Teacher usually asks for concise visual decks with clear logic progression.','general',1.0,'inferred',?,?);`, now, now)
	if err := svc.SaveWorkingMemory(ctx, SaveWorkingMemoryRequest{
		SessionID: sessID,
		UserID:    "user_u1",
		ExtractedElements: model.TeachingElements{
			KnowledgePoints: []string{"derivatives"},
			TeachingGoals:   []string{"compare derivatives and slopes"},
			TargetAudience:  "grade 11 students",
			Duration:        "45 minutes",
		},
		RecentTopics: []string{"limits"},
	}); err != nil {
		t.Fatalf("save working memory: %v", err)
	}

	durableOnly, err := svc.Recall(ctx, MemoryRecallRequest{
		UserID:    "user_u1",
		SessionID: sessID,
		Query:     "what is my teaching style preference",
		TopK:      3,
	})
	if err != nil {
		t.Fatalf("recall durable-only: %v", err)
	}
	if strings.Contains(durableOnly.ProfileSummary, "Current session:") {
		t.Fatalf("non-continuation query should keep profile summary durable-first without session snapshot")
	}

	continuation, err := svc.Recall(ctx, MemoryRecallRequest{
		UserID:    "user_u1",
		SessionID: sessID,
		Query:     "continue this lesson using current context",
		TopK:      3,
	})
	if err != nil {
		t.Fatalf("recall continuation: %v", err)
	}
	if !strings.Contains(continuation.ProfileSummary, "Current session:") {
		t.Fatalf("continuation query should include compact session addon in profile summary")
	}
	if len([]rune(continuation.ProfileSummary)) > 320 {
		t.Fatalf("profile summary should be compact and capped")
	}
}

func TestRecallTopKDefaultStillAppliesWhenNonPositive(t *testing.T) {
	svc, db := setupMemoryService(t)
	now := util.NowMilli()
	for i := 0; i < 12; i++ {
		execSQL(t, db, `INSERT INTO memory_entries (id,user_id,category,key,value,context,confidence,source,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?);`,
			fmt.Sprintf("mem_pref_%d", i), "user_u1", "preference", fmt.Sprintf("style_%d", i), "style preference", "general", 0.9, "explicit", now, now)
	}
	resp, err := svc.Recall(context.Background(), MemoryRecallRequest{
		UserID: "user_u1",
		Query:  "style preference",
		TopK:   0,
	})
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if len(resp.Facts)+len(resp.Preferences) > 5 {
		t.Fatalf("top_k default should keep compactness budget at 5 when request top_k<=0")
	}
}

func TestFormatRecallCallbackSummaryIsDeterministicAndCompact(t *testing.T) {
	resp := MemoryRecallResponse{
		ProfileSummary: "Subject: Mathematics | Visual preference: output_style=简洁蓝色 | History: teaching logic=先讲概念再做例题 | Current session: Lesson topic=勾股定理",
		Preferences: []model.MemoryEntry{
			{Key: "teaching_style", Value: "interactive"},
			{Key: "color_scheme", Value: "deep blue"},
		},
		Facts: []model.MemoryEntry{
			{Key: "subject", Value: "math teacher"},
			{Key: "school_context", Value: "high school"},
		},
	}
	first := FormatRecallCallbackSummary(resp)
	second := FormatRecallCallbackSummary(resp)
	if first != second {
		t.Fatalf("expected deterministic callback summary")
	}
	if len([]rune(first)) > recallCallbackSummaryBudget {
		t.Fatalf("expected callback summary budget <= %d, got %d", recallCallbackSummaryBudget, len([]rune(first)))
	}
	if strings.Contains(first, "\"preferences\"") || strings.Contains(first, "\"facts\"") {
		t.Fatalf("callback summary should not serialize the raw sync payload")
	}
	if strings.Contains(first, "History:") || strings.Contains(first, "Current session:") || strings.Contains(first, "teaching logic=") {
		t.Fatalf("callback summary should suppress low-signal internal phrasing, got %q", first)
	}
}

func TestProcessRecallSendsCallbackWithTransportCompatibilityMapping(t *testing.T) {
	svc, db := setupMemoryService(t)
	now := util.NowMilli()
	execSQL(t, db, `INSERT INTO memory_entries (id,user_id,category,key,value,context,confidence,source,created_at,updated_at) VALUES ('mem_pref','user_u1','preference','teaching_style','interactive','general',1.0,'explicit',?,?);`, now, now)
	execSQL(t, db, `INSERT INTO memory_entries (id,user_id,category,key,value,context,confidence,source,created_at,updated_at) VALUES ('mem_fact','user_u1','fact','subject','math teacher','general',1.0,'explicit',?,?);`, now, now)

	client := &fakeVoiceAgentClient{}
	svc.SetVoiceAgentClient(client)

	err := svc.ProcessRecall(context.Background(), RecallJob{
		RequestID: "req_123",
		UserID:    "user_u1",
		SessionID: "sess_callback",
		Query:     "teaching style",
		TopK:      3,
	})
	if err != nil {
		t.Fatalf("process recall: %v", err)
	}
	if len(client.requests) != 1 {
		t.Fatalf("expected one callback request, got %d", len(client.requests))
	}
	got := client.requests[0]
	if got.TaskID != "sess_callback" || got.SessionID != "sess_callback" {
		t.Fatalf("expected task/session transport mapping, got %#v", got)
	}
	if got.RequestID != "req_123" || got.EventType != "get_memory" {
		t.Fatalf("unexpected callback metadata: %#v", got)
	}
	if strings.TrimSpace(got.Summary) == "" {
		t.Fatalf("expected non-empty callback summary")
	}
	if len([]rune(got.Summary)) > recallCallbackSummaryBudget {
		t.Fatalf("expected compact callback summary, got %d runes", len([]rune(got.Summary)))
	}
}
