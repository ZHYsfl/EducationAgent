# Interface Service

## 数据库表字段

### 1) `sessions`

- `id` (PK, `varchar(64)`)
- `user_id` (`varchar(64)`, not null, index `idx_sessions_user`)
- `title` (`varchar(256)`, default `''`)
- `status` (`varchar(16)`, default `'active'`)
- `created_at` (`bigint/int64`, not null)
- `updated_at` (`bigint/int64`, not null)

---

### 2) `files`

- `id` (PK, `varchar(64)`)
- `user_id` (`varchar(64)`, not null, index `idx_files_user`)
- `session_id` (nullable, `varchar(64)`, index `idx_files_session`)
- `task_id` (nullable, `varchar(64)`, index `idx_files_task`)
- `filename` (`varchar(256)`, not null)
- `file_type` (`varchar(16)`, not null)
- `file_size` (`bigint/int64`, not null)
- `storage_url` (`varchar(1024)`, not null)
- `object_key` (`varchar(1024)`, not null, default `''`)
- `checksum` (`varchar(128)`, not null, default `''`) ← 新增
- `purpose` (`varchar(32)`, default `'reference'`)
- `created_at` (`bigint/int64`, not null)

---

### 3) `file_delete_jobs`

- `id` (PK, `varchar(64)`)
- `file_id` (`varchar(64)`, not null, index `idx_file_delete_jobs_file_id`)
- `storage_url` (`varchar(1024)`, not null)
- `object_key` (`varchar(1024)`, not null, default `''`)
- `status` (`varchar(16)`, not null, default `'pending'`, index `idx_file_delete_jobs_status`)
- `retry_count` (`int`, not null, default `0`)
- `last_error` (`text`, default `''`)
- `created_at` (`bigint/int64`, not null)
- `updated_at` (`bigint/int64`, not null)

---

### 4) `search_requests`

- `request_id` (PK, `varchar(64)`)
- `user_id` (`varchar(64)`, not null, index `idx_search_user`)
- `query` (`text`, not null)
- `status` (`varchar(16)`, not null, index `idx_search_status`)
- `results` (`text`)
- `summary` (`text`)
- `duration` (`bigint/int64`, not null, default `0`)
- `created_at` (`bigint/int64`, not null)
- `updated_at` (`bigint/int64`, not null)
