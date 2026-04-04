package pptx

import (
	"archive/zip"
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type ImageApplyReport struct {
	AppliedCount int      `json:"applied_count"`
	SkippedCount int      `json:"skipped_count"`
	Messages     []string `json:"messages,omitempty"`
}

type slideRels struct {
	Relationships []slideRel `xml:"Relationship"`
}

type slideRel struct {
	ID     string `xml:"Id,attr"`
	Target string `xml:"Target,attr"`
	Type   string `xml:"Type,attr"`
}

var (
	rePicBlock  = regexp.MustCompile(`(?s)<p:pic\b.*?</p:pic>`)
	reCNvPrID   = regexp.MustCompile(`<p:cNvPr\b[^>]*\bid="(\d+)"`)
	reBlipEmbed = regexp.MustCompile(`<a:blip\b[^>]*\br:embed="([^"]+)"`)
)

func normalizeZipPath(p string) string {
	return strings.TrimPrefix(filepath.ToSlash(filepath.Clean(strings.ReplaceAll(p, "\\", "/"))), "/")
}

func resolveTargetPath(relsPath, target string) string {
	base := filepath.ToSlash(filepath.Dir(relsPath))
	return normalizeZipPath(filepath.ToSlash(filepath.Clean(filepath.Join(base, target))))
}

func readZipEntries(pptxPath string) (map[string][]byte, error) {
	r, err := zip.OpenReader(pptxPath)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	out := map[string][]byte{}
	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			return nil, err
		}
		out[normalizeZipPath(f.Name)] = b
	}
	return out, nil
}

func writeZipEntries(dst string, entries map[string][]byte) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	w, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer w.Close()
	zw := zip.NewWriter(w)
	for name, b := range entries {
		fw, err := zw.Create(name)
		if err != nil {
			_ = zw.Close()
			return err
		}
		if _, err = fw.Write(b); err != nil {
			_ = zw.Close()
			return err
		}
	}
	return zw.Close()
}

func imageExt(p string) string {
	return strings.ToLower(filepath.Ext(strings.TrimSpace(p)))
}

func readImageBytes(v, baseDir string) ([]byte, string, error) {
	s := strings.TrimSpace(v)
	if s == "" {
		return nil, "", fmt.Errorf("empty image value")
	}
	if strings.HasPrefix(strings.ToLower(s), "data:image/") {
		idx := strings.Index(s, ",")
		if idx <= 0 {
			return nil, "", fmt.Errorf("invalid data uri")
		}
		meta := strings.ToLower(s[:idx])
		raw := s[idx+1:]
		b, err := base64.StdEncoding.DecodeString(raw)
		if err != nil {
			return nil, "", err
		}
		ext := ".png"
		switch {
		case strings.Contains(meta, "image/jpeg"), strings.Contains(meta, "image/jpg"):
			ext = ".jpg"
		case strings.Contains(meta, "image/webp"):
			ext = ".webp"
		case strings.Contains(meta, "image/gif"):
			ext = ".gif"
		}
		return b, ext, nil
	}
	if strings.HasPrefix(strings.ToLower(s), "http://") || strings.HasPrefix(strings.ToLower(s), "https://") {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, s, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, "", err
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, "", fmt.Errorf("http status %d", resp.StatusCode)
		}
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, "", err
		}
		ext := imageExt(s)
		if ext == "" {
			ct := strings.ToLower(resp.Header.Get("Content-Type"))
			switch {
			case strings.Contains(ct, "jpeg"):
				ext = ".jpg"
			case strings.Contains(ct, "png"):
				ext = ".png"
			case strings.Contains(ct, "webp"):
				ext = ".webp"
			case strings.Contains(ct, "gif"):
				ext = ".gif"
			}
		}
		return b, ext, nil
	}
	p := s
	if !filepath.IsAbs(p) {
		p = filepath.Join(baseDir, p)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, "", err
	}
	return b, imageExt(p), nil
}

func parseShapeRIDBySlideXML(slideXML []byte) map[int]string {
	out := map[int]string{}
	matches := rePicBlock.FindAll(slideXML, -1)
	for _, m := range matches {
		block := string(m)
		idm := reCNvPrID.FindStringSubmatch(block)
		ridm := reBlipEmbed.FindStringSubmatch(block)
		if len(idm) < 2 || len(ridm) < 2 {
			continue
		}
		id, err := strconv.Atoi(strings.TrimSpace(idm[1]))
		if err != nil || id <= 0 {
			continue
		}
		out[id] = strings.TrimSpace(ridm[1])
	}
	return out
}

func parseSlideRels(data []byte) map[string]slideRel {
	var sr slideRels
	if err := xml.Unmarshal(data, &sr); err != nil {
		return nil
	}
	out := map[string]slideRel{}
	for _, r := range sr.Relationships {
		out[r.ID] = r
	}
	return out
}

// ApplyImagesByPlan 将 image_plan 中的图片值按候选槽位顺序应用到 PPTX。
// 规则：仅覆盖已存在 media 目标文件；扩展名不一致时跳过，避免破坏 [Content_Types]。
func ApplyImagesByPlan(srcPPTX, dstPPTX string, plans []ImageApplyPlan, baseDir string) (ImageApplyReport, error) {
	rep := ImageApplyReport{Messages: []string{}}
	if len(plans) == 0 {
		rep.Messages = append(rep.Messages, "no image plans")
		return rep, nil
	}
	entries, err := readZipEntries(srcPPTX)
	if err != nil {
		return rep, err
	}
	for _, p := range plans {
		if p.SlideIndex <= 0 || len(p.ImageValues) == 0 || len(p.CandidateSlotIDs) == 0 {
			continue
		}
		slideXMLPath := normalizeZipPath(fmt.Sprintf("ppt/slides/slide%d.xml", p.SlideIndex))
		relsPath := normalizeZipPath(fmt.Sprintf("ppt/slides/_rels/slide%d.xml.rels", p.SlideIndex))
		slideXML, ok1 := entries[slideXMLPath]
		relsXML, ok2 := entries[relsPath]
		if !ok1 || !ok2 {
			rep.SkippedCount++
			rep.Messages = append(rep.Messages, fmt.Sprintf("slide %d missing xml/rels", p.SlideIndex))
			continue
		}
		shapeRID := parseShapeRIDBySlideXML(slideXML)
		rels := parseSlideRels(relsXML)
		if len(shapeRID) == 0 || len(rels) == 0 {
			rep.SkippedCount++
			rep.Messages = append(rep.Messages, fmt.Sprintf("slide %d no pic mappings", p.SlideIndex))
			continue
		}
		n := len(p.ImageValues)
		if n > len(p.CandidateSlotIDs) {
			n = len(p.CandidateSlotIDs)
		}
		for i := 0; i < n; i++ {
			slot := p.CandidateSlotIDs[i]
			val := p.ImageValues[i]
			rid := shapeRID[slot]
			if rid == "" {
				rep.SkippedCount++
				rep.Messages = append(rep.Messages, fmt.Sprintf("slide %d slot %d no embed rid", p.SlideIndex, slot))
				continue
			}
			rel, ok := rels[rid]
			if !ok {
				rep.SkippedCount++
				rep.Messages = append(rep.Messages, fmt.Sprintf("slide %d rid %s no relationship", p.SlideIndex, rid))
				continue
			}
			target := resolveTargetPath(relsPath, rel.Target)
			old, ok := entries[target]
			if !ok || len(old) == 0 {
				rep.SkippedCount++
				rep.Messages = append(rep.Messages, fmt.Sprintf("slide %d target %s missing", p.SlideIndex, target))
				continue
			}
			newBytes, srcExt, err := readImageBytes(val, baseDir)
			if err != nil || len(newBytes) == 0 {
				rep.SkippedCount++
				rep.Messages = append(rep.Messages, fmt.Sprintf("slide %d read image failed: %v", p.SlideIndex, err))
				continue
			}
			dstExt := imageExt(target)
			if srcExt != "" && dstExt != "" && srcExt != dstExt {
				rep.SkippedCount++
				rep.Messages = append(rep.Messages, fmt.Sprintf("slide %d ext mismatch %s -> %s", p.SlideIndex, srcExt, dstExt))
				continue
			}
			entries[target] = newBytes
			rep.AppliedCount++
		}
	}
	if err := writeZipEntries(dstPPTX, entries); err != nil {
		return rep, err
	}
	if len(rep.Messages) > 60 {
		rep.Messages = rep.Messages[:60]
		rep.Messages = append(rep.Messages, "messages truncated")
	}
	return rep, nil
}

func MarshalImageApplyReport(rep ImageApplyReport) any {
	b, err := json.Marshal(rep)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	var out any
	_ = json.Unmarshal(b, &out)
	return out
}

