package pptserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"educationagent/ppt_agent_service_go/internal/api"
	"educationagent/ppt_agent_service_go/internal/auth"
	"educationagent/ppt_agent_service_go/internal/config"
	"educationagent/ppt_agent_service_go/internal/dbclient"
	"educationagent/ppt_agent_service_go/internal/ecode"
	"educationagent/ppt_agent_service_go/internal/payload"
	"educationagent/ppt_agent_service_go/internal/redisx"
	"educationagent/ppt_agent_service_go/internal/task"
	"educationagent/ppt_agent_service_go/internal/toolllm"
)

var allowedTaskStatus = map[string]struct{}{
	"pending": {}, "generating": {}, "completed": {}, "failed": {}, "exporting": {},
}

var allowedIntentActions = map[string]struct{}{
	"modify": {}, "insert_before": {}, "insert_after": {}, "delete": {},
	"global_modify": {}, "resolve_conflict": {},
}

type Server struct {
	Cfg    config.Config
	DB     *dbclient.Client
	Redis  *redisx.Store
	Store  *task.Store
	LLM    *toolllm.Client
	HC     *http.Client
	RunsAbs string
	// deckRunning 防止同一任务并发执行 runGeneration（初始化生成与结构反馈重跑互斥）。
	deckRunning sync.Map
	// mergeBucketMu：每 (task_id, bucket) 一把互斥锁，对齐 Python merge_lock。
	mergeBucketMu sync.Map
	// suspendWatchers：悬挂页定时器取消函数。
	suspendWatchers sync.Map
}

func New(cfg config.Config) (*Server, error) {
	if strings.TrimSpace(cfg.DBServiceURL) == "" {
		return nil, fmt.Errorf("PPT_DATABASE_SERVICE_URL 必填")
	}
	runsAbs, err := filepath.Abs(cfg.RunsDir)
	if err != nil {
		runsAbs = cfg.RunsDir
	}
	_ = os.MkdirAll(runsAbs, 0o755)
	red, err := redisx.Connect(cfg.RedisURL, cfg.SnapshotTTL)
	if err != nil {
		red = &redisx.Store{OK: false}
	}
	llm, err := toolllm.New(cfg)
	if err != nil {
		return nil, err
	}
	return &Server{
		Cfg:     cfg,
		DB:      dbclient.New(cfg.DBServiceURL, cfg.InternalKey, cfg.DBTimeoutSec),
		Redis:   red,
		Store:   task.NewStore(runsAbs),
		LLM:     llm,
		HC:      &http.Client{Timeout: time.Duration(cfg.DBTimeoutSec) * time.Second},
		RunsAbs: runsAbs,
	}, nil
}

func (s *Server) Close() error {
	s.suspendWatchers.Range(func(key, value any) bool {
		if fn, ok := value.(context.CancelFunc); ok {
			fn()
		}
		s.suspendWatchers.Delete(key)
		return true
	})
	if s.Redis != nil {
		return s.Redis.Close()
	}
	return nil
}

func (s *Server) persist(ctx context.Context, t *task.Task) {
	if s.DB == nil || t == nil {
		return
	}
	_ = s.DB.SaveTask(ctx, payload.TaskToSaveMap(t))
}

func (s *Server) getTask(ctx context.Context, id string) *task.Task {
	if t := s.Store.TryGet(id); t != nil {
		return t
	}
	if s.DB == nil {
		return nil
	}
	m, err := s.DB.LoadTask(ctx, id)
	if err != nil || m == nil {
		return nil
	}
	t := task.NewTask()
	_ = payload.ApplyWire(t, m)
	s.Store.Set(id, t)
	return t
}

func (s *Server) assertUserMatch(w http.ResponseWriter, r *http.Request, bodyUID string) bool {
	if !s.Cfg.Enforced() {
		return false
	}
	ac := auth.FromRequest(r)
	if ac == nil || ac.IsInternal {
		return false
	}
	if strings.TrimSpace(bodyUID) != strings.TrimSpace(ac.UserID) {
		api.WriteErr(w, ecode.AuthUserMismatch, "请求中的 user_id 与 JWT 不一致")
		return true
	}
	return false
}

func (s *Server) assertTaskAccess(w http.ResponseWriter, r *http.Request, t *task.Task) bool {
	if t == nil || !s.Cfg.Enforced() {
		return false
	}
	ac := auth.FromRequest(r)
	if ac == nil || ac.IsInternal {
		return false
	}
	if ac.UserID != "" && t.UserID != ac.UserID {
		api.WriteErr(w, ecode.AuthForbiddenTask, "无权访问该任务")
		return true
	}
	return false
}

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Get("/healthz", s.healthz)
	fs := http.FileServer(http.Dir(s.RunsAbs))
	r.Handle("/static/runs/*", http.StripPrefix("/static/runs/", fs))

	r.Route("/", func(r chi.Router) {
		r.Use(auth.Middleware(s.Cfg))
		r.Post("/api/v1/ppt/init", s.pptInit)
		r.Post("/api/v1/ppt/vad", s.vad)
		r.Post("/api/v1/canvas/vad-event", s.vad)
		r.Post("/api/v1/ppt/feedback", s.feedback)
		r.Get("/api/v1/canvas/status", s.canvasStatus)
		r.Get("/api/v1/tasks/{taskID}/preview", s.taskPreview)
		r.Post("/api/v1/tasks", s.createTask)
		r.Get("/api/v1/tasks/{taskID}", s.getTaskDetail)
		r.Put("/api/v1/tasks/{taskID}/status", s.updateTaskStatus)
		r.Get("/api/v1/tasks", s.listTasks)
		r.Post("/api/v1/ppt/export", s.pptExport)
		r.Get("/api/v1/ppt/export/{exportID}", s.getExport)
		r.Get("/api/v1/ppt/page/{pageID}/render", s.pageRender)
		r.Post("/api/v1/ppt/canvas/render/execute", s.renderExecute)
		r.Post("/api/v1/kb/parse", s.kbParse)
		r.Post("/api/v1/sessions", s.proxyDB)
		r.Get("/api/v1/sessions/{sessionID}", s.proxyDB)
		r.Get("/api/v1/sessions", s.proxyDB)
		r.Put("/api/v1/sessions/{sessionID}", s.proxyDB)
		r.Patch("/api/v1/sessions/{sessionID}", s.proxyDB)
	})
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		api.WriteErr(w, ecode.NotFound, "资源不存在")
	})
	return r
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	ok := false
	if s.Redis != nil {
		ok = s.Redis.Ping(r.Context())
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok": true, "redis": ok, "ppt_agent_port": s.Cfg.Port, "implementation": "ppt_agent_service_go",
	})
}

func (s *Server) proxyDB(w http.ResponseWriter, r *http.Request) {
	base := strings.TrimRight(s.Cfg.DBServiceURL, "/")
	target := base + r.URL.Path
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	body, _ := io.ReadAll(r.Body)
	req, err := http.NewRequestWithContext(r.Context(), r.Method, target, bytes.NewReader(body))
	if err != nil {
		api.WriteErr(w, ecode.Internal, err.Error())
		return
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	if v := r.Header.Get("Authorization"); v != "" {
		req.Header.Set("Authorization", v)
	}
	if v := r.Header.Get("X-Internal-Key"); v != "" {
		req.Header.Set("X-Internal-Key", v)
	}
	resp, err := s.HC.Do(req)
	if err != nil {
		api.WriteErr(w, ecode.Dependency, "Database Service 不可达: "+err.Error())
		return
	}
	defer resp.Body.Close()
	for k, vv := range resp.Header {
		if strings.EqualFold(k, "Content-Length") {
			continue
		}
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}
