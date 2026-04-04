package dbservice

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"educationagent/database_service_go/internal/auth"
	"educationagent/database_service_go/internal/dbredis"
	"educationagent/database_service_go/internal/ecodes"
)

func dbEnabled() bool {
	v := strings.TrimSpace(os.Getenv("PPT_DATABASE_ENABLED"))
	return v == "" || v == "1" || strings.EqualFold(v, "true")
}

// DBEnabled 供 main 判断是否连接 Redis（与 Python PPT_DATABASE_ENABLED 默认 true 一致）。
func DBEnabled() bool { return dbEnabled() }

var allowedSession = map[string]struct{}{
	"active": {}, "completed": {}, "archived": {},
}

var allowedTaskStatus = map[string]struct{}{
	"pending": {}, "generating": {}, "completed": {}, "failed": {}, "exporting": {},
}

// Server 挂载 Database Service 全部路由。
type Server struct {
	R Store
}

type Store interface {
	EnsureUser(ctx context.Context, userID, displayName string) error
	EnsureSession(ctx context.Context, sessionID, userID, title string) error
	CreateSession(ctx context.Context, userID, title string) (string, error)
	GetSession(ctx context.Context, sessionID string) (*dbredis.SessionSummary, error)
	ListSessionsPaged(ctx context.Context, userID string, page, pageSize int) ([]dbredis.SessionSummary, int, error)
	UpdateSession(ctx context.Context, sessionID string, title *string, status *string) (bool, error)
	SaveTask(ctx context.Context, doc map[string]interface{}) error
	TaskWireGET(ctx context.Context, taskID string) (map[string]interface{}, error)
	ListTasksLight(ctx context.Context, sessionID string, page, pageSize int, filterUser *string) ([]dbredis.TaskListRow, int, error)
	SaveExport(ctx context.Context, e dbredis.ExportState) error
	LoadExport(ctx context.Context, exportID string) (*dbredis.ExportState, error)
	SaveFile(ctx context.Context, f dbredis.FileMeta) error
	GetFile(ctx context.Context, fileID string) (*dbredis.FileMeta, error)
	DeleteFile(ctx context.Context, fileID string) (bool, error)
}

func (s *Server) requireRepo(w http.ResponseWriter) bool {
	if s.R != nil && dbEnabled() {
		return true
	}
	writeErr(w, ecodes.Dependency, "持久化未启用或数据库未初始化（PPT_DATABASE_ENABLED）")
	return false
}

func (s *Server) ctx() context.Context {
	return context.Background()
}

func bodyUserMismatch(w http.ResponseWriter, ctx context.Context, bodyUID string) bool {
	if !auth.Enforced() {
		return false
	}
	ac := auth.FromRequest(ctx)
	if ac == nil || ac.IsInternal {
		return false
	}
	uid := strings.TrimSpace(bodyUID)
	if uid == "" {
		return false
	}
	if uid != strings.TrimSpace(ac.UserID) {
		writeErr(w, ecodes.AuthUserMismatch, "请求中的 user_id 与 JWT 不一致")
		return true
	}
	return false
}

func sessionForbidden(w http.ResponseWriter, ctx context.Context, owner string) bool {
	if !auth.Enforced() {
		return false
	}
	ac := auth.FromRequest(ctx)
	if ac == nil || ac.IsInternal {
		return false
	}
	if strings.TrimSpace(ac.UserID) != strings.TrimSpace(owner) {
		writeErr(w, ecodes.AuthForbidden, "无权访问该会话")
		return true
	}
	return false
}

func verifyUserAllowlist(userID string) string {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("PPT_USER_VERIFY")))
	if mode != "allowlist" {
		return ""
	}
	raw := os.Getenv("PPT_USER_ALLOWLIST")
	allow := map[string]struct{}{}
	for _, x := range strings.Split(raw, ",") {
		x = strings.TrimSpace(x)
		if x != "" {
			allow[x] = struct{}{}
		}
	}
	if len(allow) == 0 {
		return ""
	}
	if _, ok := allow[strings.TrimSpace(userID)]; !ok {
		return "user_id 不存在"
	}
	return ""
}

// RegisterRoutes 注册到 mux（已在外层套好 AuthMiddleware）。
func (s *Server) RegisterRoutes(r chi.Router) {
	r.Get("/healthz", s.healthz)
	r.Get("/", s.root)

	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/sessions", s.createSession)
		r.Get("/sessions/{sessionID}", s.getSession)
		r.Get("/sessions", s.listSessions)
		r.Put("/sessions/{sessionID}", s.updateSession)
		r.Post("/tasks", s.createTask)
		r.Get("/tasks/{taskID}", s.getTask)
		r.Put("/tasks/{taskID}/status", s.updateTaskStatus)
		r.Get("/tasks", s.listTasks)
		r.Post("/files/upload", s.uploadFile)
		r.Get("/files/{fileID}", s.getFile)
		r.Delete("/files/{fileID}", s.deleteFile)
	})

	r.Route("/internal/db", func(r chi.Router) {
		r.Post("/ensure-user", s.internalEnsureUser)
		r.Post("/ensure-session", s.internalEnsureSession)
		r.Post("/create-session", s.internalCreateSession)
		r.Get("/sessions/{sessionID}", s.internalGetSession)
		r.Get("/sessions", s.internalListSessions)
		r.Patch("/sessions/{sessionID}", s.internalPatchSession)
		r.Put("/tasks/{taskID}", s.internalPutTask)
		r.Get("/tasks/{taskID}", s.internalGetTask)
		r.Get("/task-list", s.internalTaskList)
		r.Put("/exports/{exportID}", s.internalPutExport)
		r.Get("/exports/{exportID}", s.internalGetExport)
	})
}

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"ok","service":"database_service"}`))
}

func (s *Server) root(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"service":"database_service","port_hint":9500}`))
}

// --- §7.3.7–7.3.10 ---

type createSessionBody struct {
	UserID string `json:"user_id"`
	Title  string `json:"title"`
}

func (s *Server) createSession(w http.ResponseWriter, r *http.Request) {
	if !s.requireRepo(w) {
		return
	}
	var body createSessionBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, ecodes.Param, "请求体无效")
		return
	}
	uid := strings.TrimSpace(body.UserID)
	if uid == "" {
		writeErr(w, ecodes.Param, "参数 user_id 不能为空")
		return
	}
	if bodyUserMismatch(w, r.Context(), uid) {
		return
	}
	if msg := verifyUserAllowlist(uid); msg != "" {
		writeErr(w, ecodes.NotFound, msg)
		return
	}
	sid, err := s.R.CreateSession(s.ctx(), uid, body.Title)
	if err != nil {
		writeErr(w, ecodes.Internal, err.Error())
		return
	}
	writeBiz(w, ecodes.OK, "success", map[string]string{"session_id": sid})
}

func (s *Server) getSession(w http.ResponseWriter, r *http.Request) {
	if !s.requireRepo(w) {
		return
	}
	sid := strings.TrimSpace(chi.URLParam(r, "sessionID"))
	if sid == "" {
		writeErr(w, ecodes.Param, "session_id 无效")
		return
	}
	row, err := s.R.GetSession(s.ctx(), sid)
	if err != nil {
		writeErr(w, ecodes.Internal, err.Error())
		return
	}
	if row == nil {
		writeErr(w, ecodes.NotFound, "session_id 不存在")
		return
	}
	if sessionForbidden(w, r.Context(), row.UserID) {
		return
	}
	writeBiz(w, ecodes.OK, "success", map[string]interface{}{
		"session_id": row.SessionID, "user_id": row.UserID, "title": row.Title,
		"status": row.Status, "created_at": row.CreatedAt, "updated_at": row.UpdatedAt,
	})
}

func (s *Server) listSessions(w http.ResponseWriter, r *http.Request) {
	if !s.requireRepo(w) {
		return
	}
	uid := strings.TrimSpace(r.URL.Query().Get("user_id"))
	if uid == "" {
		writeErr(w, ecodes.Param, "缺少或无效的查询参数 user_id")
		return
	}
	if bodyUserMismatch(w, r.Context(), uid) {
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	ps, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if page <= 0 {
		page = 1
	}
	if ps <= 0 {
		ps = 20
	}
	items, total, err := s.R.ListSessionsPaged(s.ctx(), uid, page, ps)
	if err != nil {
		writeErr(w, ecodes.Internal, err.Error())
		return
	}
	p := max(1, page)
	pss := max(1, min(ps, 100))
	list := make([]map[string]interface{}, 0, len(items))
	for _, it := range items {
		list = append(list, map[string]interface{}{
			"session_id": it.SessionID, "user_id": it.UserID, "title": it.Title,
			"status": it.Status, "created_at": it.CreatedAt, "updated_at": it.UpdatedAt,
		})
	}
	writeBiz(w, ecodes.OK, "success", map[string]interface{}{
		"total": total, "page": p, "page_size": pss, "items": list,
	})
}

type updateSessionBody struct {
	Title  *string `json:"title"`
	Status *string `json:"status"`
}

func (s *Server) updateSession(w http.ResponseWriter, r *http.Request) {
	if !s.requireRepo(w) {
		return
	}
	sid := strings.TrimSpace(chi.URLParam(r, "sessionID"))
	if sid == "" {
		writeErr(w, ecodes.Param, "session_id 无效")
		return
	}
	var body updateSessionBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, ecodes.Param, "请求体无效")
		return
	}
	if body.Title == nil && body.Status == nil {
		writeErr(w, ecodes.Param, "至少需要提供 title 或 status 之一")
		return
	}
	if body.Status != nil {
		st := strings.TrimSpace(*body.Status)
		if _, ok := allowedSession[st]; !ok {
			writeErr(w, ecodes.Param, "非法 status")
			return
		}
	}
	row, err := s.R.GetSession(s.ctx(), sid)
	if err != nil {
		writeErr(w, ecodes.Internal, err.Error())
		return
	}
	if row == nil {
		writeErr(w, ecodes.NotFound, "session_id 不存在")
		return
	}
	if sessionForbidden(w, r.Context(), row.UserID) {
		return
	}
	var tPtr, stPtr *string
	if body.Title != nil {
		t := strings.TrimSpace(*body.Title)
		tPtr = &t
	}
	if body.Status != nil {
		st := strings.TrimSpace(*body.Status)
		stPtr = &st
	}
	ok, err := s.R.UpdateSession(s.ctx(), sid, tPtr, stPtr)
	if err != nil || !ok {
		writeErr(w, ecodes.NotFound, "session_id 不存在")
		return
	}
	row2, _ := s.R.GetSession(s.ctx(), sid)
	writeBiz(w, ecodes.OK, "success", map[string]interface{}{
		"session_id": row2.SessionID, "user_id": row2.UserID, "title": row2.Title,
		"status": row2.Status, "created_at": row2.CreatedAt, "updated_at": row2.UpdatedAt,
	})
}

type createTaskBody struct {
	SessionID   string `json:"session_id"`
	UserID      string `json:"user_id"`
	Topic       string `json:"topic"`
	Description string `json:"description"`
	TotalPages  int    `json:"total_pages"`
	Audience    string `json:"audience"`
	GlobalStyle string `json:"global_style"`
}

func newTaskID() string { return "task_" + uuid.New().String() }

func (s *Server) createTask(w http.ResponseWriter, r *http.Request) {
	if !s.requireRepo(w) {
		return
	}
	var body createTaskBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, ecodes.Param, "请求体无效")
		return
	}
	body.UserID = strings.TrimSpace(body.UserID)
	body.SessionID = strings.TrimSpace(body.SessionID)
	body.Topic = strings.TrimSpace(body.Topic)
	body.Description = strings.TrimSpace(body.Description)
	if body.UserID == "" {
		writeErr(w, ecodes.Param, "参数 user_id 不能为空")
		return
	}
	if bodyUserMismatch(w, r.Context(), body.UserID) {
		return
	}
	if body.SessionID == "" {
		writeErr(w, ecodes.Param, "参数 session_id 不能为空")
		return
	}
	if body.Topic == "" {
		writeErr(w, ecodes.Param, "参数 topic 不能为空")
		return
	}
	if body.Description == "" {
		writeErr(w, ecodes.Param, "参数 description 不能为空")
		return
	}
	if body.TotalPages < 0 {
		writeErr(w, ecodes.Param, "total_pages 不能为负")
		return
	}
	if msg := verifyUserAllowlist(body.UserID); msg != "" {
		writeErr(w, ecodes.NotFound, msg)
		return
	}
	// session 必须存在且属于该 user。
	row, err := s.R.GetSession(s.ctx(), body.SessionID)
	if err != nil {
		writeErr(w, ecodes.Internal, err.Error())
		return
	}
	if row == nil || strings.TrimSpace(row.UserID) != body.UserID {
		writeErr(w, ecodes.NotFound, "session_id 不存在")
		return
	}
	tid := newTaskID()
	doc := map[string]interface{}{
		"id":           tid,
		"session_id":   body.SessionID,
		"user_id":      body.UserID,
		"topic":        body.Topic,
		"description":  body.Description,
		"total_pages":  body.TotalPages,
		"audience":     strings.TrimSpace(body.Audience),
		"global_style": strings.TrimSpace(body.GlobalStyle),
		"status":       "pending",
		"agent_payload": map[string]interface{}{
			"id":         tid,
			"session_id": body.SessionID,
			"user_id":    body.UserID,
			"topic":      body.Topic,
		},
	}
	if err := s.R.SaveTask(s.ctx(), doc); err != nil {
		writeErr(w, ecodes.Internal, err.Error())
		return
	}
	writeBiz(w, ecodes.OK, "success", map[string]string{"task_id": tid})
}

func (s *Server) getTask(w http.ResponseWriter, r *http.Request) {
	if !s.requireRepo(w) {
		return
	}
	tid := strings.TrimSpace(chi.URLParam(r, "taskID"))
	if tid == "" {
		writeErr(w, ecodes.Param, "task_id 无效")
		return
	}
	wire, err := s.R.TaskWireGET(s.ctx(), tid)
	if err != nil {
		writeErr(w, ecodes.Internal, err.Error())
		return
	}
	if wire == nil {
		writeErr(w, ecodes.NotFound, "task_id 不存在")
		return
	}
	if sessionForbidden(w, r.Context(), str(wire["user_id"])) {
		return
	}
	writeBiz(w, ecodes.OK, "success", map[string]interface{}{
		"task_id":      str(wire["id"]),
		"session_id":   str(wire["session_id"]),
		"user_id":      str(wire["user_id"]),
		"topic":        str(wire["topic"]),
		"description":  str(wire["description"]),
		"total_pages":  int64Val(wire["total_pages"]),
		"audience":     str(wire["audience"]),
		"global_style": str(wire["global_style"]),
		"status":       str(wire["status"]),
		"created_at":   int64Val(wire["created_at"]),
		"updated_at":   int64Val(wire["updated_at"]),
	})
}

type updateTaskStatusBody struct {
	Status string `json:"status"`
}

func (s *Server) updateTaskStatus(w http.ResponseWriter, r *http.Request) {
	if !s.requireRepo(w) {
		return
	}
	tid := strings.TrimSpace(chi.URLParam(r, "taskID"))
	if tid == "" {
		writeErr(w, ecodes.Param, "task_id 无效")
		return
	}
	var body updateTaskStatusBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, ecodes.Param, "请求体无效")
		return
	}
	st := strings.TrimSpace(body.Status)
	if _, ok := allowedTaskStatus[st]; !ok {
		writeErr(w, ecodes.Param, "非法 status")
		return
	}
	wire, err := s.R.TaskWireGET(s.ctx(), tid)
	if err != nil {
		writeErr(w, ecodes.Internal, err.Error())
		return
	}
	if wire == nil {
		writeErr(w, ecodes.NotFound, "task_id 不存在")
		return
	}
	if sessionForbidden(w, r.Context(), str(wire["user_id"])) {
		return
	}
	doc := map[string]interface{}{
		"id":           tid,
		"session_id":   str(wire["session_id"]),
		"user_id":      str(wire["user_id"]),
		"topic":        str(wire["topic"]),
		"description":  str(wire["description"]),
		"total_pages":  int64Val(wire["total_pages"]),
		"audience":     str(wire["audience"]),
		"global_style": str(wire["global_style"]),
		"status":       st,
		"updated_at":   time.Now().UnixMilli(),
		"agent_payload": wire["agent_payload"],
	}
	if err := s.R.SaveTask(s.ctx(), doc); err != nil {
		writeErr(w, ecodes.Internal, err.Error())
		return
	}
	writeBiz(w, ecodes.OK, "success", map[string]string{"task_id": tid, "status": st})
}

func (s *Server) listTasks(w http.ResponseWriter, r *http.Request) {
	if !s.requireRepo(w) {
		return
	}
	sid := strings.TrimSpace(r.URL.Query().Get("session_id"))
	if sid == "" {
		writeErr(w, ecodes.Param, "缺少或无效的查询参数 session_id")
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	ps, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if page <= 0 {
		page = 1
	}
	if ps <= 0 {
		ps = 20
	}
	rows, total, err := s.R.ListTasksLight(s.ctx(), sid, page, ps, nil)
	if err != nil {
		writeErr(w, ecodes.Internal, err.Error())
		return
	}
	list := make([]map[string]interface{}, 0, len(rows))
	for _, x := range rows {
		if sessionForbidden(w, r.Context(), x.UserID) {
			return
		}
		list = append(list, map[string]interface{}{
			"task_id":     x.TaskID,
			"session_id":  x.SessionID,
			"user_id":     x.UserID,
			"topic":       x.Topic,
			"status":      x.Status,
			"updated_at":  x.UpdatedAt,
		})
	}
	writeBiz(w, ecodes.OK, "success", map[string]interface{}{
		"items":     list,
		"total":     total,
		"page":      page,
		"page_size": ps,
	})
}

func uploadDir() string {
	d := strings.TrimSpace(os.Getenv("PPT_FILE_UPLOAD_DIR"))
	if d == "" {
		d = "uploads"
	}
	return d
}

func detectFileTypeFromName(name string) string {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
	switch ext {
	case "pdf":
		return "pdf"
	case "docx", "doc":
		return "docx"
	case "pptx", "ppt":
		return "pptx"
	case "jpg", "jpeg", "png", "webp", "gif", "bmp":
		return "image"
	case "mp4", "webm", "mov", "avi", "mkv":
		return "video"
	case "html", "htm":
		return "html"
	default:
		return ext
	}
}

func saveUploadedPart(dir string, fh *multipart.FileHeader) (string, int64, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", 0, err
	}
	src, err := fh.Open()
	if err != nil {
		return "", 0, err
	}
	defer src.Close()
	dstPath := filepath.Join(dir, filepath.Base(fh.Filename))
	dst, err := os.Create(dstPath)
	if err != nil {
		return "", 0, err
	}
	defer dst.Close()
	n, err := io.Copy(dst, src)
	if err != nil {
		return "", 0, err
	}
	return dstPath, n, nil
}

func (s *Server) uploadFile(w http.ResponseWriter, r *http.Request) {
	if !s.requireRepo(w) {
		return
	}
	if err := r.ParseMultipartForm(600 << 20); err != nil {
		writeErr(w, ecodes.Param, "multipart 请求无效")
		return
	}
	userID := strings.TrimSpace(r.FormValue("user_id"))
	sessionID := strings.TrimSpace(r.FormValue("session_id"))
	taskID := strings.TrimSpace(r.FormValue("task_id"))
	purpose := strings.TrimSpace(r.FormValue("purpose"))
	if purpose == "" {
		purpose = "reference"
	}
	if userID == "" {
		ac := auth.FromRequest(r.Context())
		if ac != nil && !ac.IsInternal {
			userID = strings.TrimSpace(ac.UserID)
		}
	}
	if userID == "" {
		writeErr(w, ecodes.Param, "参数 user_id 不能为空")
		return
	}
	if bodyUserMismatch(w, r.Context(), userID) {
		return
	}
	if msg := verifyUserAllowlist(userID); msg != "" {
		writeErr(w, ecodes.NotFound, msg)
		return
	}
	file, fh, err := r.FormFile("file")
	if err != nil || fh == nil {
		writeErr(w, ecodes.Param, "缺少文件字段 file")
		return
	}
	_ = file.Close()

	fid := dbredis.NewFileID()
	targetDir := filepath.Join(uploadDir(), userID, purpose, fid)
	realPath, n, err := saveUploadedPart(targetDir, fh)
	if err != nil {
		writeErr(w, ecodes.Internal, err.Error())
		return
	}
	ft := detectFileTypeFromName(fh.Filename)
	meta := dbredis.FileMeta{
		FileID:     fid,
		UserID:     userID,
		SessionID:  sessionID,
		TaskID:     taskID,
		Filename:   fh.Filename,
		FileType:   ft,
		FileSize:   n,
		StorageURL: filepath.ToSlash(realPath),
		Purpose:    purpose,
		CreatedAt:  time.Now().UnixMilli(),
	}
	if err := s.R.SaveFile(s.ctx(), meta); err != nil {
		writeErr(w, ecodes.Internal, err.Error())
		return
	}
	writeBiz(w, ecodes.OK, "success", map[string]interface{}{
		"file_id":     meta.FileID,
		"filename":    meta.Filename,
		"file_type":   meta.FileType,
		"file_size":   meta.FileSize,
		"storage_url": meta.StorageURL,
		"purpose":     meta.Purpose,
	})
}

func (s *Server) getFile(w http.ResponseWriter, r *http.Request) {
	if !s.requireRepo(w) {
		return
	}
	fid := strings.TrimSpace(chi.URLParam(r, "fileID"))
	if fid == "" {
		writeErr(w, ecodes.Param, "file_id 无效")
		return
	}
	meta, err := s.R.GetFile(s.ctx(), fid)
	if err != nil {
		writeErr(w, ecodes.Internal, err.Error())
		return
	}
	if meta == nil {
		writeErr(w, ecodes.NotFound, "file_id 不存在")
		return
	}
	if sessionForbidden(w, r.Context(), meta.UserID) {
		return
	}
	writeBiz(w, ecodes.OK, "success", map[string]interface{}{
		"file_id":      meta.FileID,
		"user_id":      meta.UserID,
		"session_id":   meta.SessionID,
		"task_id":      meta.TaskID,
		"filename":     meta.Filename,
		"file_type":    meta.FileType,
		"file_size":    meta.FileSize,
		"storage_url":  meta.StorageURL,
		"download_url": meta.StorageURL,
		"purpose":      meta.Purpose,
		"created_at":   meta.CreatedAt,
	})
}

func (s *Server) deleteFile(w http.ResponseWriter, r *http.Request) {
	if !s.requireRepo(w) {
		return
	}
	fid := strings.TrimSpace(chi.URLParam(r, "fileID"))
	if fid == "" {
		writeErr(w, ecodes.Param, "file_id 无效")
		return
	}
	meta, err := s.R.GetFile(s.ctx(), fid)
	if err != nil {
		writeErr(w, ecodes.Internal, err.Error())
		return
	}
	if meta == nil {
		writeErr(w, ecodes.NotFound, "file_id 不存在")
		return
	}
	if sessionForbidden(w, r.Context(), meta.UserID) {
		return
	}
	ok, err := s.R.DeleteFile(s.ctx(), fid)
	if err != nil {
		writeErr(w, ecodes.Internal, err.Error())
		return
	}
	if ok {
		_ = os.Remove(meta.StorageURL)
	}
	writeBiz(w, ecodes.OK, "deleted", map[string]interface{}{"file_id": fid})
}

// --- internal ---

func (s *Server) internalEnsureUser(w http.ResponseWriter, r *http.Request) {
	if !s.requireRepo(w) {
		return
	}
	var body map[string]interface{}
	_ = json.NewDecoder(r.Body).Decode(&body)
	uid := str(body["user_id"])
	dn := str(body["display_name"])
	_ = s.R.EnsureUser(s.ctx(), uid, dn)
	writeRawJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) internalEnsureSession(w http.ResponseWriter, r *http.Request) {
	if !s.requireRepo(w) {
		return
	}
	var body map[string]interface{}
	_ = json.NewDecoder(r.Body).Decode(&body)
	_ = s.R.EnsureSession(s.ctx(), str(body["session_id"]), str(body["user_id"]), str(body["title"]))
	writeRawJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) internalCreateSession(w http.ResponseWriter, r *http.Request) {
	if !s.requireRepo(w) {
		return
	}
	var body map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, ecodes.Param, "请求体无效")
		return
	}
	uid := strings.TrimSpace(str(body["user_id"]))
	if uid == "" {
		writeErr(w, ecodes.Param, "user_id 必填")
		return
	}
	sid, err := s.R.CreateSession(s.ctx(), uid, str(body["title"]))
	if err != nil {
		writeErr(w, ecodes.Internal, err.Error())
		return
	}
	writeRawJSON(w, http.StatusOK, map[string]string{"session_id": sid})
}

func (s *Server) internalGetSession(w http.ResponseWriter, r *http.Request) {
	if !s.requireRepo(w) {
		return
	}
	sid := strings.TrimSpace(chi.URLParam(r, "sessionID"))
	row, err := s.R.GetSession(s.ctx(), sid)
	if err != nil {
		writeErr(w, ecodes.Internal, err.Error())
		return
	}
	if row == nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	writeRawJSON(w, http.StatusOK, map[string]interface{}{
		"session_id": row.SessionID, "user_id": row.UserID, "title": row.Title,
		"status": row.Status, "created_at": row.CreatedAt, "updated_at": row.UpdatedAt,
	})
}

func (s *Server) internalListSessions(w http.ResponseWriter, r *http.Request) {
	if !s.requireRepo(w) {
		return
	}
	uid := strings.TrimSpace(r.URL.Query().Get("user_id"))
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	ps, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	items, total, err := s.R.ListSessionsPaged(s.ctx(), uid, page, ps)
	if err != nil {
		writeErr(w, ecodes.Internal, err.Error())
		return
	}
	list := make([]map[string]interface{}, 0, len(items))
	for _, it := range items {
		list = append(list, map[string]interface{}{
			"session_id": it.SessionID, "user_id": it.UserID, "title": it.Title,
			"status": it.Status, "created_at": it.CreatedAt, "updated_at": it.UpdatedAt,
		})
	}
	writeRawJSON(w, http.StatusOK, map[string]interface{}{"items": list, "total": total})
}

func (s *Server) internalPatchSession(w http.ResponseWriter, r *http.Request) {
	if !s.requireRepo(w) {
		return
	}
	sid := strings.TrimSpace(chi.URLParam(r, "sessionID"))
	var body map[string]interface{}
	_ = json.NewDecoder(r.Body).Decode(&body)
	var tPtr, stPtr *string
	if _, ok := body["title"]; ok {
		v := str(body["title"])
		tPtr = &v
	}
	if _, ok := body["status"]; ok {
		v := str(body["status"])
		stPtr = &v
	}
	if tPtr == nil && stPtr == nil {
		writeRawJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	ok, err := s.R.UpdateSession(s.ctx(), sid, tPtr, stPtr)
	if err != nil || !ok {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	writeRawJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) internalPutTask(w http.ResponseWriter, r *http.Request) {
	if !s.requireRepo(w) {
		return
	}
	tid := strings.TrimSpace(chi.URLParam(r, "taskID"))
	var body map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, ecodes.Param, "请求体无效")
		return
	}
	body["id"] = tid
	if err := s.R.SaveTask(s.ctx(), body); err != nil {
		writeErr(w, ecodes.Internal, err.Error())
		return
	}
	writeRawJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) internalGetTask(w http.ResponseWriter, r *http.Request) {
	if !s.requireRepo(w) {
		return
	}
	tid := strings.TrimSpace(chi.URLParam(r, "taskID"))
	wire, err := s.R.TaskWireGET(s.ctx(), tid)
	if err != nil {
		writeErr(w, ecodes.Internal, err.Error())
		return
	}
	if wire == nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}
	writeRawJSON(w, http.StatusOK, wire)
}

func (s *Server) internalTaskList(w http.ResponseWriter, r *http.Request) {
	if !s.requireRepo(w) {
		return
	}
	sid := strings.TrimSpace(r.URL.Query().Get("session_id"))
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	ps, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	uidq := strings.TrimSpace(r.URL.Query().Get("user_id"))
	var uidPtr *string
	if uidq != "" {
		uidPtr = &uidq
	}
	rows, total, err := s.R.ListTasksLight(s.ctx(), sid, page, ps, uidPtr)
	if err != nil {
		writeErr(w, ecodes.Internal, err.Error())
		return
	}
	list := make([]map[string]interface{}, 0, len(rows))
	for _, x := range rows {
		list = append(list, map[string]interface{}{
			"task_id": x.TaskID, "session_id": x.SessionID, "user_id": x.UserID,
			"topic": x.Topic, "status": x.Status, "updated_at": x.UpdatedAt,
		})
	}
	writeRawJSON(w, http.StatusOK, map[string]interface{}{"items": list, "total": total})
}

func (s *Server) internalPutExport(w http.ResponseWriter, r *http.Request) {
	if !s.requireRepo(w) {
		return
	}
	eid := strings.TrimSpace(chi.URLParam(r, "exportID"))
	var body map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, ecodes.Param, "请求体无效")
		return
	}
	body["export_id"] = eid
	e := dbredis.ExportFromWire(body)
	if err := s.R.SaveExport(s.ctx(), e); err != nil {
		writeErr(w, ecodes.Internal, err.Error())
		return
	}
	writeRawJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) internalGetExport(w http.ResponseWriter, r *http.Request) {
	if !s.requireRepo(w) {
		return
	}
	eid := strings.TrimSpace(chi.URLParam(r, "exportID"))
	e, err := s.R.LoadExport(s.ctx(), eid)
	if err != nil {
		writeErr(w, ecodes.Internal, err.Error())
		return
	}
	if e == nil {
		http.Error(w, "export not found", http.StatusNotFound)
		return
	}
	writeRawJSON(w, http.StatusOK, dbredis.ExportToPersistWire(e))
}

func writeRawJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func str(v interface{}) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatInt(int64(t), 10)
	case json.Number:
		return t.String()
	default:
		return fmt.Sprint(t)
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
