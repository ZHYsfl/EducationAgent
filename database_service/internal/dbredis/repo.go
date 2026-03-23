package dbredis

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

func fixRedisURL() string {
	if u := strings.TrimSpace(os.Getenv("PPT_DB_REDIS_URL")); u != "" {
		return u
	}
	if u := strings.TrimSpace(os.Getenv("REDIS_URL")); u != "" {
		return u
	}
	return "redis://127.0.0.1:6379/0"
}

func prefix() string {
	p := strings.TrimSpace(os.Getenv("PPT_DB_REDIS_PREFIX"))
	if p == "" {
		return "ppt_db:"
	}
	return p
}

func utcMs() int64 {
	return time.Now().UnixMilli()
}

// Repo 与 Python RedisPPTRepository 键空间一致。
type Repo struct {
	r *redis.Client
	p string
}

func Connect(ctx context.Context) (*Repo, error) {
	opt, err := redis.ParseURL(fixRedisURL())
	if err != nil {
		return nil, fmt.Errorf("redis url: %w", err)
	}
	c := redis.NewClient(opt)
	if err := c.Ping(ctx).Err(); err != nil {
		_ = c.Close()
		return nil, err
	}
	return &Repo{r: c, p: prefix()}, nil
}

func (rp *Repo) Close() error {
	return rp.r.Close()
}

func newSessionID() string {
	return "sess_" + uuid.New().String()
}

func (rp *Repo) userKey(uid string) string   { return rp.p + "user:" + uid }
func (rp *Repo) sessionKey(sid string) string { return rp.p + "session:" + sid }
func (rp *Repo) userSessionsZ(uid string) string {
	return rp.p + "user_sessions:" + uid
}
func (rp *Repo) taskKey(tid string) string { return rp.p + "task:" + tid }
func (rp *Repo) sessionTasksZ(sid string) string {
	return rp.p + "session_tasks:" + sid
}
func (rp *Repo) exportKey(eid string) string { return rp.p + "export:" + eid }
func (rp *Repo) fileKey(fid string) string   { return rp.p + "file:" + fid }
func (rp *Repo) userFilesZ(uid string) string {
	return rp.p + "user_files:" + uid
}

func (rp *Repo) EnsureUser(ctx context.Context, userID, displayName string) error {
	uid := strings.TrimSpace(userID)
	if uid == "" {
		return nil
	}
	now := utcMs()
	key := rp.userKey(uid)
	ok, err := rp.r.Exists(ctx, key).Result()
	if err != nil || ok > 0 {
		return err
	}
	dn := strings.TrimSpace(displayName)
	if dn == "" {
		dn = uid
	}
	doc := map[string]interface{}{
		"id": uid, "username": uid, "email": uid + "@ppt-agent.local",
		"password_hash": "$noop$", "display_name": truncate(dn, 128),
		"subject": "", "school": "", "role": "teacher",
		"created_at": now, "updated_at": now,
	}
	b, _ := json.Marshal(doc)
	return rp.r.Set(ctx, key, b, 0).Err()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func (rp *Repo) EnsureSession(ctx context.Context, sessionID, userID, title string) error {
	sid := strings.TrimSpace(sessionID)
	uid := strings.TrimSpace(userID)
	if sid == "" || uid == "" {
		return nil
	}
	if err := rp.EnsureUser(ctx, uid, ""); err != nil {
		return err
	}
	now := utcMs()
	raw, err := rp.r.Get(ctx, rp.sessionKey(sid)).Bytes()
	if err != nil && err != redis.Nil {
		return err
	}
	if len(raw) > 0 {
		var prev map[string]interface{}
		_ = json.Unmarshal(raw, &prev)
		if ou, ok := prev["user_id"].(string); ok && ou != uid {
			// 与 Python 一致：已存在且 user 不同则忽略 ensure
		}
		return nil
	}
	doc := map[string]interface{}{
		"id": sid, "user_id": uid, "title": truncate(strings.TrimSpace(title), 256),
		"status": "active", "created_at": now, "updated_at": now,
	}
	b, _ := json.Marshal(doc)
	if err := rp.r.Set(ctx, rp.sessionKey(sid), b, 0).Err(); err != nil {
		return err
	}
	return rp.r.ZAdd(ctx, rp.userSessionsZ(uid), redis.Z{Score: float64(now), Member: sid}).Err()
}

func (rp *Repo) CreateSession(ctx context.Context, userID, title string) (string, error) {
	uid := strings.TrimSpace(userID)
	if uid == "" {
		return "", fmt.Errorf("user_id 不能为空")
	}
	if err := rp.EnsureUser(ctx, uid, ""); err != nil {
		return "", err
	}
	sid := newSessionID()
	now := utcMs()
	doc := map[string]interface{}{
		"id": sid, "user_id": uid, "title": truncate(strings.TrimSpace(title), 256),
		"status": "active", "created_at": now, "updated_at": now,
	}
	b, _ := json.Marshal(doc)
	if err := rp.r.Set(ctx, rp.sessionKey(sid), b, 0).Err(); err != nil {
		return "", err
	}
	if err := rp.r.ZAdd(ctx, rp.userSessionsZ(uid), redis.Z{Score: float64(now), Member: sid}).Err(); err != nil {
		return "", err
	}
	return sid, nil
}

type SessionSummary struct {
	SessionID string
	UserID    string
	Title     string
	Status    string
	CreatedAt int64
	UpdatedAt int64
}

func (rp *Repo) GetSession(ctx context.Context, sessionID string) (*SessionSummary, error) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return nil, nil
	}
	raw, err := rp.r.Get(ctx, rp.sessionKey(sid)).Bytes()
	if err == redis.Nil || len(raw) == 0 {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var row map[string]interface{}
	if err := json.Unmarshal(raw, &row); err != nil {
		return nil, err
	}
	return &SessionSummary{
		SessionID: strVal(row["id"]),
		UserID:    strVal(row["user_id"]),
		Title:     strVal(row["title"]),
		Status:    strOr(row["status"], "active"),
		CreatedAt: int64Val(row["created_at"]),
		UpdatedAt: int64Val(row["updated_at"]),
	}, nil
}

func (rp *Repo) ListSessionsPaged(ctx context.Context, userID string, page, pageSize int) ([]SessionSummary, int, error) {
	uid := strings.TrimSpace(userID)
	p := max(1, page)
	ps := max(1, min(pageSize, 100))
	offset := (p - 1) * ps
	zkey := rp.userSessionsZ(uid)
	total, err := rp.r.ZCard(ctx, zkey).Result()
	if err != nil {
		return nil, 0, err
	}
	ids, err := rp.r.ZRevRange(ctx, zkey, int64(offset), int64(offset+ps-1)).Result()
	if err != nil {
		return nil, 0, err
	}
	var items []SessionSummary
	for _, id := range ids {
		s, err := rp.GetSession(ctx, id)
		if err != nil || s == nil {
			continue
		}
		items = append(items, *s)
	}
	return items, int(total), nil
}

func (rp *Repo) UpdateSession(ctx context.Context, sessionID string, title *string, status *string) (bool, error) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return false, nil
	}
	raw, err := rp.r.Get(ctx, rp.sessionKey(sid)).Bytes()
	if err == redis.Nil || len(raw) == 0 {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var doc map[string]interface{}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return false, err
	}
	now := utcMs()
	if title != nil {
		doc["title"] = truncate(strings.TrimSpace(*title), 256)
	}
	if status != nil {
		doc["status"] = truncate(strings.TrimSpace(*status), 16)
	}
	doc["updated_at"] = now
	uid := strVal(doc["user_id"])
	b, _ := json.Marshal(doc)
	if err := rp.r.Set(ctx, rp.sessionKey(sid), b, 0).Err(); err != nil {
		return false, err
	}
	_ = rp.r.ZAdd(ctx, rp.userSessionsZ(uid), redis.Z{Score: float64(now), Member: sid}).Err()
	return true, nil
}

// SaveTask 写入与 Python save_task 相同 JSON 形状（agent_payload 为 JSON 字符串字段）。
func (rp *Repo) SaveTask(ctx context.Context, doc map[string]interface{}) error {
	tid := strings.TrimSpace(strVal(doc["id"]))
	sess := strings.TrimSpace(strVal(doc["session_id"]))
	uid := strings.TrimSpace(strVal(doc["user_id"]))
	if tid == "" || sess == "" || uid == "" {
		return fmt.Errorf("task id/session_id/user_id 必填")
	}
	if err := rp.EnsureSession(ctx, sess, uid, ""); err != nil {
		return err
	}
	now := utcMs()
	rawOld, _ := rp.r.Get(ctx, rp.taskKey(tid)).Bytes()
	var prev map[string]interface{}
	if len(rawOld) > 0 {
		_ = json.Unmarshal(rawOld, &prev)
	}
	var oldSid string
	if prev != nil {
		oldSid = strVal(prev["session_id"])
	}
	apStr, err := normalizeAgentPayload(doc["agent_payload"])
	if err != nil {
		return err
	}
	updatedAt := max64(now, int64Val(doc["updated_at"]))
	if updatedAt <= 0 {
		updatedAt = now
	}
	createdAt := now
	if prev != nil {
		createdAt = int64Val(prev["created_at"])
		if createdAt <= 0 {
			createdAt = now
		}
	}
	out := map[string]interface{}{
		"id":             tid,
		"session_id":     sess,
		"user_id":        uid,
		"topic":          truncate(strVal(doc["topic"]), 256),
		"description":    strVal(doc["description"]),
		"total_pages":    intVal(doc["total_pages"]),
		"audience":       truncate(strVal(doc["audience"]), 128),
		"global_style":   truncate(strVal(doc["global_style"]), 128),
		"status":         strOr(doc["status"], "pending"),
		"created_at":     createdAt,
		"updated_at":     updatedAt,
		"agent_payload":  apStr,
	}
	if oldSid != "" && oldSid != sess {
		_ = rp.r.ZRem(ctx, rp.sessionTasksZ(oldSid), tid).Err()
	}
	b, _ := json.Marshal(out)
	if err := rp.r.Set(ctx, rp.taskKey(tid), b, 0).Err(); err != nil {
		return err
	}
	return rp.r.ZAdd(ctx, rp.sessionTasksZ(sess), redis.Z{Score: float64(updatedAt), Member: tid}).Err()
}

// TaskWireGET 返回任务持久化字段；对外接口按规范透出 created_at/updated_at。
func (rp *Repo) TaskWireGET(ctx context.Context, taskID string) (map[string]interface{}, error) {
	tid := strings.TrimSpace(taskID)
	if tid == "" {
		return nil, nil
	}
	raw, err := rp.r.Get(ctx, rp.taskKey(tid)).Bytes()
	if err == redis.Nil || len(raw) == 0 {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var row map[string]interface{}
	if err := json.Unmarshal(raw, &row); err != nil {
		return nil, err
	}
	ua := int64Val(row["updated_at"])
	ca := int64Val(row["created_at"])
	wire := map[string]interface{}{
		"id":             row["id"],
		"session_id":     row["session_id"],
		"user_id":        row["user_id"],
		"topic":          row["topic"],
		"description":    row["description"],
		"total_pages":    row["total_pages"],
		"audience":       row["audience"],
		"global_style":   row["global_style"],
		"status":         row["status"],
		"created_at":     ca,
		"updated_at":     ua,
		"agent_payload":  row["agent_payload"],
	}
	return wire, nil
}

type TaskListRow struct {
	TaskID    string
	SessionID string
	UserID    string
	Topic     string
	Status    string
	UpdatedAt int64
}

func (rp *Repo) ListTasksLight(ctx context.Context, sessionID string, page, pageSize int, filterUser *string) ([]TaskListRow, int, error) {
	sid := strings.TrimSpace(sessionID)
	p := max(1, page)
	ps := max(1, min(pageSize, 100))
	offset := (p - 1) * ps
	zkey := rp.sessionTasksZ(sid)
	ids, err := rp.r.ZRevRange(ctx, zkey, 0, -1).Result()
	if err != nil {
		return nil, 0, err
	}
	uidf := ""
	if filterUser != nil {
		uidf = strings.TrimSpace(*filterUser)
	}
	var rows []TaskListRow
	for _, id := range ids {
		raw, err := rp.r.Get(ctx, rp.taskKey(id)).Bytes()
		if err != nil || len(raw) == 0 {
			continue
		}
		var d map[string]interface{}
		if json.Unmarshal(raw, &d) != nil {
			continue
		}
		if uidf != "" && strVal(d["user_id"]) != uidf {
			continue
		}
		rows = append(rows, TaskListRow{
			TaskID:    strVal(d["id"]),
			SessionID: strVal(d["session_id"]),
			UserID:    strVal(d["user_id"]),
			Topic:     strVal(d["topic"]),
			Status:    strOr(d["status"], "pending"),
			UpdatedAt: int64Val(d["updated_at"]),
		})
	}
	total := len(rows)
	end := min(offset+ps, total)
	if offset >= total {
		return nil, total, nil
	}
	return rows[offset:end], total, nil
}

type ExportState struct {
	ExportID    string
	TaskID      string
	Format      string
	Status      string
	DownloadURL string
	FileSize    int64
	LastUpdate  int64
}

type FileMeta struct {
	FileID     string
	UserID     string
	SessionID  string
	TaskID     string
	Filename   string
	FileType   string
	FileSize   int64
	StorageURL string
	Purpose    string
	CreatedAt  int64
}

func (rp *Repo) SaveExport(ctx context.Context, e ExportState) error {
	now := e.LastUpdate
	if now <= 0 {
		now = utcMs()
	}
	doc := map[string]interface{}{
		"id": e.ExportID, "task_id": e.TaskID,
		"export_format": truncate(e.Format, 16),
		"status":        strOr(e.Status, "pending"),
		"download_url":  e.DownloadURL,
		"file_size":     e.FileSize,
		"last_update":   now,
	}
	b, _ := json.Marshal(doc)
	return rp.r.Set(ctx, rp.exportKey(e.ExportID), b, 0).Err()
}

func (rp *Repo) LoadExport(ctx context.Context, exportID string) (*ExportState, error) {
	eid := strings.TrimSpace(exportID)
	if eid == "" {
		return nil, nil
	}
	raw, err := rp.r.Get(ctx, rp.exportKey(eid)).Bytes()
	if err == redis.Nil || len(raw) == 0 {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var row map[string]interface{}
	if err := json.Unmarshal(raw, &row); err != nil {
		return nil, err
	}
	return &ExportState{
		ExportID:    strVal(row["id"]),
		TaskID:      strVal(row["task_id"]),
		Format:      strVal(row["export_format"]),
		Status:      strOr(row["status"], "pending"),
		DownloadURL: strVal(row["download_url"]),
		FileSize:    int64Val(row["file_size"]),
		LastUpdate:  int64Val(row["last_update"]),
	}, nil
}

func NewFileID() string { return "file_" + uuid.New().String() }

func (rp *Repo) SaveFile(ctx context.Context, f FileMeta) error {
	if strings.TrimSpace(f.FileID) == "" || strings.TrimSpace(f.UserID) == "" {
		return fmt.Errorf("file_id/user_id 必填")
	}
	now := f.CreatedAt
	if now <= 0 {
		now = utcMs()
	}
	doc := map[string]interface{}{
		"id":          strings.TrimSpace(f.FileID),
		"user_id":     strings.TrimSpace(f.UserID),
		"session_id":  strings.TrimSpace(f.SessionID),
		"task_id":     strings.TrimSpace(f.TaskID),
		"filename":    truncate(strings.TrimSpace(f.Filename), 256),
		"file_type":   truncate(strings.TrimSpace(f.FileType), 32),
		"file_size":   f.FileSize,
		"storage_url": strings.TrimSpace(f.StorageURL),
		"purpose":     truncate(strings.TrimSpace(f.Purpose), 32),
		"created_at":  now,
	}
	b, _ := json.Marshal(doc)
	if err := rp.r.Set(ctx, rp.fileKey(f.FileID), b, 0).Err(); err != nil {
		return err
	}
	return rp.r.ZAdd(ctx, rp.userFilesZ(f.UserID), redis.Z{Score: float64(now), Member: f.FileID}).Err()
}

func (rp *Repo) GetFile(ctx context.Context, fileID string) (*FileMeta, error) {
	fid := strings.TrimSpace(fileID)
	if fid == "" {
		return nil, nil
	}
	raw, err := rp.r.Get(ctx, rp.fileKey(fid)).Bytes()
	if err == redis.Nil || len(raw) == 0 {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var row map[string]interface{}
	if err := json.Unmarshal(raw, &row); err != nil {
		return nil, err
	}
	return &FileMeta{
		FileID:     strVal(row["id"]),
		UserID:     strVal(row["user_id"]),
		SessionID:  strVal(row["session_id"]),
		TaskID:     strVal(row["task_id"]),
		Filename:   strVal(row["filename"]),
		FileType:   strVal(row["file_type"]),
		FileSize:   int64Val(row["file_size"]),
		StorageURL: strVal(row["storage_url"]),
		Purpose:    strVal(row["purpose"]),
		CreatedAt:  int64Val(row["created_at"]),
	}, nil
}

func (rp *Repo) DeleteFile(ctx context.Context, fileID string) (bool, error) {
	meta, err := rp.GetFile(ctx, fileID)
	if err != nil {
		return false, err
	}
	if meta == nil {
		return false, nil
	}
	if err := rp.r.Del(ctx, rp.fileKey(fileID)).Err(); err != nil {
		return false, err
	}
	_ = rp.r.ZRem(ctx, rp.userFilesZ(meta.UserID), fileID).Err()
	return true, nil
}

// ExportToPersistWire 与 Python export_to_persist_wire 对齐。
func ExportToPersistWire(e *ExportState) map[string]interface{} {
	return map[string]interface{}{
		"export_id":    e.ExportID,
		"task_id":      e.TaskID,
		"format":       e.Format,
		"status":       e.Status,
		"download_url": e.DownloadURL,
		"file_size":    e.FileSize,
		"last_update":  e.LastUpdate,
	}
}

func ExportFromWire(d map[string]interface{}) ExportState {
	return ExportState{
		ExportID:    strVal(d["export_id"]),
		TaskID:      strVal(d["task_id"]),
		Format:      strVal(d["format"]),
		Status:      strOr(d["status"], "pending"),
		DownloadURL: strVal(d["download_url"]),
		FileSize:    int64Val(d["file_size"]),
		LastUpdate:  int64Val(d["last_update"]),
	}
}

func normalizeAgentPayload(v interface{}) (string, error) {
	if v == nil {
		return "{}", nil
	}
	switch t := v.(type) {
	case string:
		if strings.TrimSpace(t) == "" {
			return "{}", nil
		}
		return t, nil
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
}

func strVal(v interface{}) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case json.Number:
		return t.String()
	case float64:
		return strconv.FormatInt(int64(t), 10)
	default:
		return fmt.Sprint(t)
	}
}

func strOr(v interface{}, def string) string {
	s := strVal(v)
	if s == "" {
		return def
	}
	return s
}

func int64Val(v interface{}) int64 {
	if v == nil {
		return 0
	}
	switch t := v.(type) {
	case float64:
		return int64(t)
	case int64:
		return t
	case int:
		return int64(t)
	case json.Number:
		i, _ := t.Int64()
		return i
	case string:
		i, _ := strconv.ParseInt(strings.TrimSpace(t), 10, 64)
		return i
	default:
		return 0
	}
}

func intVal(v interface{}) int {
	return int(int64Val(v))
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
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
