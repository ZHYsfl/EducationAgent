package dbpg

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"

	"educationagent/database_service_go/internal/dbredis"
)

type Repo struct {
	db *sql.DB
}

func dsn() string {
	if v := strings.TrimSpace(os.Getenv("PPT_DB_PG_DSN")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("DATABASE_URL")); v != "" {
		return v
	}
	return "postgres://postgres:postgres@127.0.0.1:5432/educationagent?sslmode=disable"
}

func Connect(ctx context.Context) (*Repo, error) {
	db, err := sql.Open("postgres", dsn())
	if err != nil {
		return nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	rp := &Repo{db: db}
	if err := rp.initSchema(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return rp, nil
}

func (rp *Repo) Close() error { return rp.db.Close() }

func (rp *Repo) initSchema(ctx context.Context) error {
	ddl := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			display_name TEXT DEFAULT '',
			created_at BIGINT NOT NULL,
			updated_at BIGINT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			title TEXT DEFAULT '',
			status TEXT DEFAULT 'active',
			created_at BIGINT NOT NULL,
			updated_at BIGINT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id)`,
		`CREATE TABLE IF NOT EXISTS tasks (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			user_id TEXT NOT NULL,
			topic TEXT NOT NULL,
			description TEXT DEFAULT '',
			total_pages INT DEFAULT 0,
			audience TEXT DEFAULT '',
			global_style TEXT DEFAULT '',
			status TEXT DEFAULT 'pending',
			created_at BIGINT NOT NULL,
			updated_at BIGINT NOT NULL,
			agent_payload TEXT DEFAULT '{}'
		)`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_session ON tasks(session_id)`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_user ON tasks(user_id)`,
		`CREATE TABLE IF NOT EXISTS files (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			session_id TEXT DEFAULT '',
			task_id TEXT DEFAULT '',
			filename TEXT NOT NULL,
			file_type TEXT NOT NULL,
			file_size BIGINT NOT NULL,
			storage_url TEXT NOT NULL,
			purpose TEXT DEFAULT 'reference',
			created_at BIGINT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_files_user ON files(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_files_task ON files(task_id)`,
		`CREATE TABLE IF NOT EXISTS exports (
			id TEXT PRIMARY KEY,
			task_id TEXT NOT NULL,
			export_format TEXT NOT NULL,
			status TEXT NOT NULL,
			download_url TEXT DEFAULT '',
			file_size BIGINT DEFAULT 0,
			last_update BIGINT NOT NULL
		)`,
	}
	for _, q := range ddl {
		if _, err := rp.db.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return nil
}

func nowMS() int64 { return time.Now().UnixMilli() }

func newSessionID() string { return "sess_" + uuid.New().String() }

func (rp *Repo) EnsureUser(ctx context.Context, userID, displayName string) error {
	uid := strings.TrimSpace(userID)
	if uid == "" {
		return nil
	}
	ts := nowMS()
	_, err := rp.db.ExecContext(ctx, `
		INSERT INTO users(id, display_name, created_at, updated_at)
		VALUES($1,$2,$3,$4)
		ON CONFLICT (id) DO NOTHING
	`, uid, strings.TrimSpace(displayName), ts, ts)
	return err
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
	ts := nowMS()
	_, err := rp.db.ExecContext(ctx, `
		INSERT INTO sessions(id,user_id,title,status,created_at,updated_at)
		VALUES($1,$2,$3,'active',$4,$5)
		ON CONFLICT (id) DO NOTHING
	`, sid, uid, strings.TrimSpace(title), ts, ts)
	return err
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
	ts := nowMS()
	_, err := rp.db.ExecContext(ctx, `
		INSERT INTO sessions(id,user_id,title,status,created_at,updated_at)
		VALUES($1,$2,$3,'active',$4,$5)
	`, sid, uid, strings.TrimSpace(title), ts, ts)
	return sid, err
}

func (rp *Repo) GetSession(ctx context.Context, sessionID string) (*dbredis.SessionSummary, error) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return nil, nil
	}
	row := rp.db.QueryRowContext(ctx, `SELECT id,user_id,title,status,created_at,updated_at FROM sessions WHERE id=$1`, sid)
	var s dbredis.SessionSummary
	if err := row.Scan(&s.SessionID, &s.UserID, &s.Title, &s.Status, &s.CreatedAt, &s.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &s, nil
}

func (rp *Repo) ListSessionsPaged(ctx context.Context, userID string, page, pageSize int) ([]dbredis.SessionSummary, int, error) {
	p := max(1, page)
	ps := max(1, min(pageSize, 100))
	uid := strings.TrimSpace(userID)
	var total int
	if err := rp.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE user_id=$1`, uid).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := rp.db.QueryContext(ctx, `
		SELECT id,user_id,title,status,created_at,updated_at
		FROM sessions WHERE user_id=$1
		ORDER BY updated_at DESC
		LIMIT $2 OFFSET $3
	`, uid, ps, (p-1)*ps)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]dbredis.SessionSummary, 0)
	for rows.Next() {
		var s dbredis.SessionSummary
		if err := rows.Scan(&s.SessionID, &s.UserID, &s.Title, &s.Status, &s.CreatedAt, &s.UpdatedAt); err == nil {
			out = append(out, s)
		}
	}
	return out, total, nil
}

func (rp *Repo) UpdateSession(ctx context.Context, sessionID string, title *string, status *string) (bool, error) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return false, nil
	}
	sets := []string{"updated_at=$1"}
	args := []any{nowMS()}
	idx := 2
	if title != nil {
		sets = append(sets, fmt.Sprintf("title=$%d", idx))
		args = append(args, strings.TrimSpace(*title))
		idx++
	}
	if status != nil {
		sets = append(sets, fmt.Sprintf("status=$%d", idx))
		args = append(args, strings.TrimSpace(*status))
		idx++
	}
	args = append(args, sid)
	res, err := rp.db.ExecContext(ctx, `UPDATE sessions SET `+strings.Join(sets, ",")+fmt.Sprintf(" WHERE id=$%d", idx), args...)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
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
	case float64:
		return strconv.FormatInt(int64(t), 10)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case json.Number:
		return t.String()
	default:
		return fmt.Sprint(v)
	}
}

func intVal(v interface{}) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(t))
		return n
	case json.Number:
		i, _ := t.Int64()
		return int(i)
	default:
		return 0
	}
}

func int64Val(v interface{}) int64 {
	switch t := v.(type) {
	case int64:
		return t
	case int:
		return int64(t)
	case float64:
		return int64(t)
	case string:
		n, _ := strconv.ParseInt(strings.TrimSpace(t), 10, 64)
		return n
	case json.Number:
		i, _ := t.Int64()
		return i
	default:
		return 0
	}
}

func (rp *Repo) SaveTask(ctx context.Context, doc map[string]interface{}) error {
	tid := strings.TrimSpace(strVal(doc["id"]))
	sid := strings.TrimSpace(strVal(doc["session_id"]))
	uid := strings.TrimSpace(strVal(doc["user_id"]))
	if tid == "" || sid == "" || uid == "" {
		return fmt.Errorf("task id/session_id/user_id 必填")
	}
	if err := rp.EnsureSession(ctx, sid, uid, ""); err != nil {
		return err
	}
	payload, err := normalizeAgentPayload(doc["agent_payload"])
	if err != nil {
		return err
	}
	createdAt := int64Val(doc["created_at"])
	if createdAt <= 0 {
		createdAt = nowMS()
	}
	updatedAt := int64Val(doc["updated_at"])
	if updatedAt <= 0 {
		updatedAt = nowMS()
	}
	_, err = rp.db.ExecContext(ctx, `
		INSERT INTO tasks(id,session_id,user_id,topic,description,total_pages,audience,global_style,status,created_at,updated_at,agent_payload)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		ON CONFLICT (id) DO UPDATE SET
			session_id=EXCLUDED.session_id,
			user_id=EXCLUDED.user_id,
			topic=EXCLUDED.topic,
			description=EXCLUDED.description,
			total_pages=EXCLUDED.total_pages,
			audience=EXCLUDED.audience,
			global_style=EXCLUDED.global_style,
			status=EXCLUDED.status,
			updated_at=EXCLUDED.updated_at,
			agent_payload=EXCLUDED.agent_payload
	`, tid, sid, uid, strVal(doc["topic"]), strVal(doc["description"]), intVal(doc["total_pages"]), strVal(doc["audience"]), strVal(doc["global_style"]), strVal(doc["status"]), createdAt, updatedAt, payload)
	return err
}

func (rp *Repo) TaskWireGET(ctx context.Context, taskID string) (map[string]interface{}, error) {
	tid := strings.TrimSpace(taskID)
	if tid == "" {
		return nil, nil
	}
	row := rp.db.QueryRowContext(ctx, `
		SELECT id,session_id,user_id,topic,description,total_pages,audience,global_style,status,created_at,updated_at,agent_payload
		FROM tasks WHERE id=$1
	`, tid)
	var id, sid, uid, topic, desc, aud, gs, st, ap string
	var tp int
	var ca, ua int64
	if err := row.Scan(&id, &sid, &uid, &topic, &desc, &tp, &aud, &gs, &st, &ca, &ua, &ap); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return map[string]interface{}{
		"id": id, "session_id": sid, "user_id": uid, "topic": topic, "description": desc,
		"total_pages": tp, "audience": aud, "global_style": gs, "status": st,
		"created_at": ca, "updated_at": ua, "agent_payload": ap,
	}, nil
}

func (rp *Repo) ListTasksLight(ctx context.Context, sessionID string, page, pageSize int, filterUser *string) ([]dbredis.TaskListRow, int, error) {
	p := max(1, page)
	ps := max(1, min(pageSize, 100))
	sid := strings.TrimSpace(sessionID)
	uid := ""
	if filterUser != nil {
		uid = strings.TrimSpace(*filterUser)
	}
	where := "session_id=$1"
	args := []any{sid}
	if uid != "" {
		where += " AND user_id=$2"
		args = append(args, uid)
	}
	cntQ := `SELECT COUNT(*) FROM tasks WHERE ` + where
	var total int
	if err := rp.db.QueryRowContext(ctx, cntQ, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, ps, (p-1)*ps)
	q := `SELECT id,session_id,user_id,topic,status,updated_at FROM tasks WHERE ` + where + ` ORDER BY updated_at DESC LIMIT $` + strconv.Itoa(len(args)-1) + ` OFFSET $` + strconv.Itoa(len(args))
	rows, err := rp.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]dbredis.TaskListRow, 0)
	for rows.Next() {
		var r dbredis.TaskListRow
		if err := rows.Scan(&r.TaskID, &r.SessionID, &r.UserID, &r.Topic, &r.Status, &r.UpdatedAt); err == nil {
			out = append(out, r)
		}
	}
	return out, total, nil
}

func (rp *Repo) SaveExport(ctx context.Context, e dbredis.ExportState) error {
	ts := e.LastUpdate
	if ts <= 0 {
		ts = nowMS()
	}
	_, err := rp.db.ExecContext(ctx, `
		INSERT INTO exports(id,task_id,export_format,status,download_url,file_size,last_update)
		VALUES($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (id) DO UPDATE SET
			task_id=EXCLUDED.task_id,
			export_format=EXCLUDED.export_format,
			status=EXCLUDED.status,
			download_url=EXCLUDED.download_url,
			file_size=EXCLUDED.file_size,
			last_update=EXCLUDED.last_update
	`, e.ExportID, e.TaskID, e.Format, e.Status, e.DownloadURL, e.FileSize, ts)
	return err
}

func (rp *Repo) LoadExport(ctx context.Context, exportID string) (*dbredis.ExportState, error) {
	eid := strings.TrimSpace(exportID)
	if eid == "" {
		return nil, nil
	}
	row := rp.db.QueryRowContext(ctx, `SELECT id,task_id,export_format,status,download_url,file_size,last_update FROM exports WHERE id=$1`, eid)
	var e dbredis.ExportState
	if err := row.Scan(&e.ExportID, &e.TaskID, &e.Format, &e.Status, &e.DownloadURL, &e.FileSize, &e.LastUpdate); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &e, nil
}

func (rp *Repo) SaveFile(ctx context.Context, f dbredis.FileMeta) error {
	ts := f.CreatedAt
	if ts <= 0 {
		ts = nowMS()
	}
	_, err := rp.db.ExecContext(ctx, `
		INSERT INTO files(id,user_id,session_id,task_id,filename,file_type,file_size,storage_url,purpose,created_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT (id) DO UPDATE SET
			user_id=EXCLUDED.user_id,
			session_id=EXCLUDED.session_id,
			task_id=EXCLUDED.task_id,
			filename=EXCLUDED.filename,
			file_type=EXCLUDED.file_type,
			file_size=EXCLUDED.file_size,
			storage_url=EXCLUDED.storage_url,
			purpose=EXCLUDED.purpose
	`, f.FileID, f.UserID, f.SessionID, f.TaskID, f.Filename, f.FileType, f.FileSize, f.StorageURL, f.Purpose, ts)
	return err
}

func (rp *Repo) GetFile(ctx context.Context, fileID string) (*dbredis.FileMeta, error) {
	fid := strings.TrimSpace(fileID)
	if fid == "" {
		return nil, nil
	}
	row := rp.db.QueryRowContext(ctx, `
		SELECT id,user_id,session_id,task_id,filename,file_type,file_size,storage_url,purpose,created_at
		FROM files WHERE id=$1
	`, fid)
	var f dbredis.FileMeta
	if err := row.Scan(&f.FileID, &f.UserID, &f.SessionID, &f.TaskID, &f.Filename, &f.FileType, &f.FileSize, &f.StorageURL, &f.Purpose, &f.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &f, nil
}

func (rp *Repo) DeleteFile(ctx context.Context, fileID string) (bool, error) {
	res, err := rp.db.ExecContext(ctx, `DELETE FROM files WHERE id=$1`, strings.TrimSpace(fileID))
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
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

