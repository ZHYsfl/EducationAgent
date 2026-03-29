package model

// KBCollection 知识库集合
type KBCollection struct {
	CollectionID string `json:"collection_id"` // coll_<uuid>
	UserID       string `json:"user_id"`
	Name         string `json:"name"`
	Subject      string `json:"subject"`      // 学科：数学、物理、计算机...
	Description  string `json:"description"`
	DocCount     int    `json:"doc_count"`
	CreatedAt    int64  `json:"created_at"`
	UpdatedAt    int64  `json:"updated_at"`
}

// KBDocument 知识库文档
type KBDocument struct {
	DocID        string `json:"doc_id"`         // doc_<uuid>
	CollectionID string `json:"collection_id"`
	FileID       string `json:"file_id"`        // 关联 Database Service 的文件 ID
	Title        string `json:"title"`
	DocType      string `json:"doc_type"`       // pdf | docx | pptx | image | video | text | web_snippet
	ChunkCount   int    `json:"chunk_count"`
	Status       string `json:"status"`         // pending | processing | indexed | failed
	ErrorMessage string `json:"error_message,omitempty"`
	CreatedAt    int64  `json:"created_at"`
}

// TextChunk 文本块
type TextChunk struct {
	ChunkID  string    `json:"chunk_id"` // chunk_<uuid>
	DocID    string    `json:"doc_id"`
	Content  string    `json:"content"`  // 文本内容
	Metadata ChunkMeta `json:"metadata"`
}

// ChunkMeta 文本块元数据
type ChunkMeta struct {
	PageNumber   int    `json:"page_number,omitempty"`   // PDF/PPT 页码
	SectionTitle string `json:"section_title,omitempty"` // 章节标题
	ChunkIndex   int    `json:"chunk_index"`             // 在文档中的序号
	StartChar    int    `json:"start_char"`              // 原文起始字符位置
	EndChar      int    `json:"end_char"`
	ImageURL     string `json:"image_url,omitempty"`     // 关联图片 URL
	SourceType   string `json:"source_type"`             // text | ocr | video_transcript
	Origin       string `json:"origin,omitempty"`        // web_search | upload
	SourceURL    string `json:"source_url,omitempty"`    // web_snippet 的原始 URL（用于去重）
}

// RetrievedChunk RAG 检索结果块
type RetrievedChunk struct {
	ChunkID  string    `json:"chunk_id"`
	DocID    string    `json:"doc_id"`
	DocTitle string    `json:"doc_title"`
	Content  string    `json:"content"`
	Score    float64   `json:"score"` // 相似度分数 0-1
	Metadata ChunkMeta `json:"metadata"`
}

// ---- 请求/响应结构体 ----

// CreateCollectionRequest POST /api/v1/kb/collections
type CreateCollectionRequest struct {
	UserID      string `json:"user_id"`
	Name        string `json:"name"`
	Subject     string `json:"subject"`
	Description string `json:"description"`
}

// IndexDocumentRequest POST /api/v1/kb/documents
type IndexDocumentRequest struct {
	CollectionID string `json:"collection_id"`
	FileID       string `json:"file_id"`
	FileURL      string `json:"file_url"`
	FileType     string `json:"file_type"`
	Title        string `json:"title"`
}

// KBQueryRequest POST /api/v1/kb/query
type KBQueryRequest struct {
	CollectionID   string            `json:"collection_id"`
	UserID         string            `json:"user_id"`
	Query          string            `json:"query"`
	TopK           int               `json:"top_k"`
	ScoreThreshold float64           `json:"score_threshold"`
	Filters        map[string]string `json:"filters,omitempty"`
}

// KBQueryResponse POST /api/v1/kb/query 响应
type KBQueryResponse struct {
	Chunks []RetrievedChunk `json:"chunks"`
	Total  int              `json:"total"`
}

// IngestFromSearchRequest POST /api/v1/kb/ingest-from-search
type IngestFromSearchRequest struct {
	UserID       string              `json:"user_id"`
	CollectionID string              `json:"collection_id,omitempty"`
	Items        []SearchIngestItem  `json:"items"`
}

// SearchIngestItem 搜索结果条目
type SearchIngestItem struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Content string `json:"content"` // 精炼后的正文内容
	Source  string `json:"source"`  // 来源网站
}

// ParseInput POST /api/v1/kb/parse 请求
type ParseInput struct {
	FileURL  string `json:"file_url"`
	FileType string `json:"file_type"`
	DocID    string `json:"doc_id"`
	Content  string `json:"content,omitempty"` // web_snippet 场景直接传文本内容，避免 FileURL 字段语义混乱
}

// ParsedDocument POST /api/v1/kb/parse 响应
type ParsedDocument struct {
	DocID      string      `json:"doc_id"`
	FileType   string      `json:"file_type"`
	Title      string      `json:"title"`
	TextChunks []TextChunk `json:"text_chunks"`
	Summary    string      `json:"summary"`
	TotalPages int         `json:"total_pages,omitempty"`
}
