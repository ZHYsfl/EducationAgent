package parseflow

import (
	"encoding/json"
	"regexp"
	"strings"
)

type ParseInput struct {
	FileURL  string      `json:"file_url"`
	FileType string      `json:"file_type"`
	DocID    string      `json:"doc_id"`
	Config   ChunkConfig `json:"chunk_config,omitempty"`
}

type ParsedDocument struct {
	DocID      string           `json:"doc_id"`
	FileType   string           `json:"file_type"`
	Title      string           `json:"title"`
	TextChunks []TextChunk      `json:"text_chunks"`
	Images     []ExtractedImage `json:"images,omitempty"`
	KeyFrames  []KeyFrame       `json:"key_frames,omitempty"`
	Tables     []ExtractedTable `json:"tables,omitempty"`
	Summary    string           `json:"summary"`
	TotalPages int              `json:"total_pages,omitempty"`
}

type TextChunk struct {
	ChunkID  string    `json:"chunk_id"`
	DocID    string    `json:"doc_id"`
	Content  string    `json:"content"`
	Metadata ChunkMeta `json:"metadata"`
}

type ChunkMeta struct {
	PageNumber   int    `json:"page_number,omitempty"`
	SectionTitle string `json:"section_title,omitempty"`
	ChunkIndex   int    `json:"chunk_index"`
	StartChar    int    `json:"start_char"`
	EndChar      int    `json:"end_char"`
	ImageURL     string `json:"image_url,omitempty"`
	SourceType   string `json:"source_type"`
}

type ExtractedImage struct {
	ImageID     string `json:"image_id"`
	ImageURL    string `json:"image_url"`
	PageNumber  int    `json:"page_number"`
	Description string `json:"description"`
	OCRText     string `json:"ocr_text"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
}

type KeyFrame struct {
	FrameID     string  `json:"frame_id"`
	ImageURL    string  `json:"image_url"`
	Timestamp   float64 `json:"timestamp"`
	Description string  `json:"description"`
	Transcript  string  `json:"transcript"`
}

type ExtractedTable struct {
	TableID    string     `json:"table_id"`
	PageNumber int        `json:"page_number"`
	Headers    []string   `json:"headers"`
	Rows       [][]string `json:"rows"`
	Markdown   string     `json:"markdown"`
}

type ChunkConfig struct {
	ChunkSize    int    `json:"chunk_size"`
	ChunkOverlap int    `json:"chunk_overlap"`
	SplitBy      string `json:"split_by"`
	MinChunkSize int    `json:"min_chunk_size"`
}

type Envelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func DefaultChunkConfig() ChunkConfig {
	return ChunkConfig{
		ChunkSize:    800,
		ChunkOverlap: 100,
		SplitBy:      "paragraph",
		MinChunkSize: 100,
	}
}

func NormalizeFileType(s string) string {
	x := strings.ToLower(strings.TrimSpace(s))
	switch x {
	case "word", "doc":
		return "docx"
	case "jpg", "jpeg", "png", "webp", "gif", "bmp":
		return "image"
	default:
		return x
	}
}

func IsSupportedFileType(s string) bool {
	switch NormalizeFileType(s) {
	case "pdf", "docx", "pptx", "image", "video", "text":
		return true
	default:
		return false
	}
}

var reWs = regexp.MustCompile(`\s+`)

func normalizeText(s string) string {
	return strings.TrimSpace(reWs.ReplaceAllString(strings.ReplaceAll(s, "\r\n", "\n"), " "))
}

func splitByMode(s, mode string) []string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "page":
		arr := strings.Split(s, "\f")
		out := make([]string, 0, len(arr))
		for _, x := range arr {
			x = strings.TrimSpace(x)
			if x != "" {
				out = append(out, x)
			}
		}
		return out
	case "heading":
		parts := regexp.MustCompile(`\n(?=#|\d+[\.\、])`).Split(s, -1)
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				out = append(out, p)
			}
		}
		return out
	default:
		parts := regexp.MustCompile(`\n{2,}`).Split(s, -1)
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				out = append(out, p)
			}
		}
		return out
	}
}

func buildChunkID(docID string, idx int) string {
	return docID + "_chunk_" + strings.TrimSpace(strings.ToLower(strings.ReplaceAll(strings.TrimSpace(strings.Repeat("0", 0)), " ", ""))) + strconvI(idx)
}

func strconvI(i int) string {
	if i == 0 {
		return "0"
	}
	d := "0123456789"
	n := i
	out := ""
	for n > 0 {
		out = string(d[n%10]) + out
		n /= 10
	}
	return out
}

func Rechunk(doc ParsedDocument, cfg ChunkConfig) ParsedDocument {
	if cfg.ChunkSize <= 0 {
		cfg = DefaultChunkConfig()
	}
	if cfg.ChunkOverlap < 0 {
		cfg.ChunkOverlap = 0
	}
	if cfg.MinChunkSize <= 0 {
		cfg.MinChunkSize = 100
	}
	doc.FileType = NormalizeFileType(doc.FileType)
	rawPieces := make([]string, 0, len(doc.TextChunks))
	for _, c := range doc.TextChunks {
		if t := normalizeText(c.Content); t != "" {
			rawPieces = append(rawPieces, t)
		}
	}
	if len(rawPieces) == 0 && strings.TrimSpace(doc.Summary) != "" {
		rawPieces = append(rawPieces, normalizeText(doc.Summary))
	}
	joined := strings.TrimSpace(strings.Join(rawPieces, "\n\n"))
	if joined == "" {
		doc.TextChunks = nil
		return doc
	}
	segments := splitByMode(joined, cfg.SplitBy)
	if len(segments) == 0 {
		segments = []string{joined}
	}
	out := make([]TextChunk, 0, len(segments))
	cursor := 0
	for _, seg := range segments {
		r := []rune(seg)
		for st := 0; st < len(r); {
			ed := st + cfg.ChunkSize
			if ed > len(r) {
				ed = len(r)
			}
			part := strings.TrimSpace(string(r[st:ed]))
			if len([]rune(part)) >= cfg.MinChunkSize || (ed == len(r) && strings.TrimSpace(part) != "") {
				out = append(out, TextChunk{
					ChunkID: buildChunkID(doc.DocID, len(out)+1),
					DocID:   doc.DocID,
					Content: part,
					Metadata: ChunkMeta{
						ChunkIndex: len(out) + 1,
						StartChar:  cursor + st,
						EndChar:    cursor + ed,
						SourceType: "text",
					},
				})
			}
			if ed == len(r) {
				break
			}
			step := cfg.ChunkSize - cfg.ChunkOverlap
			if step <= 0 {
				step = cfg.ChunkSize
			}
			st += step
		}
		cursor += len(r) + 2
	}
	doc.TextChunks = out
	return doc
}

func DecodeEnvelopeOrRaw(raw []byte, out any) error {
	var env Envelope
	if err := json.Unmarshal(raw, &env); err == nil && env.Code != 0 {
		if env.Code != 200 {
			if strings.TrimSpace(env.Message) == "" {
				env.Message = "remote parse failed"
			}
			return &RemoteErr{Code: env.Code, Message: env.Message}
		}
		if len(env.Data) == 0 || string(env.Data) == "null" {
			return nil
		}
		return json.Unmarshal(env.Data, out)
	}
	return json.Unmarshal(raw, out)
}

type RemoteErr struct {
	Code    int
	Message string
}

func (e *RemoteErr) Error() string { return e.Message }

