package pptserver

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"educationagent/ppt_agent_service_go/internal/api"
	"educationagent/ppt_agent_service_go/internal/ecode"
	"educationagent/ppt_agent_service_go/internal/parseflow"
)

func kbParseURL() string {
	if v := strings.TrimSpace(os.Getenv("KB_PARSE_URL")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("KB_SERVICE_URL")); v != "" {
		return strings.TrimRight(v, "/") + "/api/v1/kb/parse"
	}
	return ""
}

func (s *Server) kbParse(w http.ResponseWriter, r *http.Request) {
	var in parseflow.ParseInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		api.WriteErr(w, ecode.Param, api.InvalidJSONMessage(err))
		return
	}
	in.FileType = parseflow.NormalizeFileType(in.FileType)
	if strings.TrimSpace(in.FileURL) == "" {
		api.WriteErr(w, ecode.Param, "file_url 不能为空")
		return
	}
	if !parseflow.IsSupportedFileType(in.FileType) {
		api.WriteErr(w, ecode.Param, "file_type 仅支持 pdf/docx/pptx/image/video/text")
		return
	}
	if in.Config.ChunkSize <= 0 {
		in.Config = parseflow.DefaultChunkConfig()
	}
	u := kbParseURL()
	if u == "" {
		api.WriteErr(w, ecode.Dependency, "未配置 KB_PARSE_URL 或 KB_SERVICE_URL")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()
	reqBody, _ := json.Marshal(in)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	if v := r.Header.Get("Authorization"); v != "" {
		req.Header.Set("Authorization", v)
	}

	hc := s.HC
	if hc == nil {
		hc = &http.Client{Timeout: 40 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		api.WriteErr(w, ecode.Dependency, "KB parse 不可达: "+err.Error())
		return
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		api.WriteErr(w, ecode.Dependency, "KB parse 失败")
		return
	}
	var doc parseflow.ParsedDocument
	if err := parseflow.DecodeEnvelopeOrRaw(raw, &doc); err != nil {
		api.WriteErr(w, ecode.Dependency, "KB parse 响应无效: "+err.Error())
		return
	}
	if strings.TrimSpace(doc.DocID) == "" {
		doc.DocID = strings.TrimSpace(in.DocID)
	}
	if strings.TrimSpace(doc.FileType) == "" {
		doc.FileType = in.FileType
	}
	out := parseflow.Rechunk(doc, in.Config)
	api.WriteOK(w, out)
}

