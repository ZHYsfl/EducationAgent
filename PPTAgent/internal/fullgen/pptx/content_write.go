package pptx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/kenny-not-dead/gopptx"
)

type ContentSlide struct {
	Title string
	Body  string
}

type ElementPlan struct {
	Name string
	Data []string
}

type SlideElementPlan struct {
	SlideIndex int
	Elements   []ElementPlan
}

type ImageApplyPlan struct {
	SlideIndex    int      `json:"slide_index"`
	ImageValues   []string `json:"image_values,omitempty"`
	CandidateSlotIDs []int `json:"candidate_slot_ids,omitempty"`
	Note          string   `json:"note,omitempty"`
}

type textAssignment struct {
	SlideIndex int
	ShapeID    int
	Text       string
}

func joinLines(ss []string) string {
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	return strings.Join(out, "\n")
}

// BuildSlidesFromContentPlan 将 content_plan 转成简化可写入的 slide 文本。
func BuildSlidesFromContentPlan(contentPlan any) []ContentSlide {
	arr, ok := contentPlan.([]map[string]any)
	if !ok {
		// fallback: try []any
		raw, ok2 := contentPlan.([]any)
		if !ok2 {
			return nil
		}
		arr = make([]map[string]any, 0, len(raw))
		for _, v := range raw {
			if m, ok := v.(map[string]any); ok {
				arr = append(arr, m)
			}
		}
	}
	slides := make([]ContentSlide, 0, len(arr))
	for _, it := range arr {
		layout := strings.TrimSpace(fmt.Sprint(it["layout"]))
		elems, _ := it["elements"].([]any)
		title := layout
		var bodyLines []string
		for _, e := range elems {
			em, ok := e.(map[string]any)
			if !ok {
				continue
			}
			name := strings.TrimSpace(fmt.Sprint(em["name"]))
			data, _ := em["data"].([]any)
			ds := make([]string, 0, len(data))
			for _, d := range data {
				ds = append(ds, fmt.Sprint(d))
			}
			if title == "" && strings.Contains(strings.ToLower(name), "title") && len(ds) > 0 {
				title = ds[0]
				if len(ds) > 1 {
					bodyLines = append(bodyLines, ds[1:]...)
				}
				continue
			}
			if len(ds) > 0 {
				bodyLines = append(bodyLines, name+": "+joinLines(ds))
			}
		}
		if title == "" {
			title = "Slide"
		}
		slides = append(slides, ContentSlide{
			Title: title,
			Body:  strings.TrimSpace(strings.Join(bodyLines, "\n")),
		})
	}
	return slides
}

// WriteSimpleContentPPTX 使用 gopptx 生成一份“内容化”PPT（纯 Go）。
// 注意：此为过渡写入器，目标是先把 content_plan 落成可读 PPT，后续再对齐模板精确落位。
func WriteSimpleContentPPTX(dstPath string, slides []ContentSlide) error {
	if len(slides) == 0 {
		return fmt.Errorf("slides empty")
	}
	_ = os.MkdirAll(filepath.Dir(dstPath), 0o755)
	f := gopptx.NewFile()
	defer f.Close()

	// 默认模板已有一页，先改第1页标题；其余页创建新页。
	for i, s := range slides {
		var slideID int
		if i == 0 {
			slideID = 256
		} else {
			id, err := f.NewSlide()
			if err != nil {
				return err
			}
			slideID = id
		}
		titleBody := gopptx.DecodeTextBody{
			Paragraph: []gopptx.DecodeParagraph{
				{
					Runs: []gopptx.DecodeRuns{{Text: s.Title}},
				},
			},
		}
		_ = f.SetShapeTextBody(slideID, 7, titleBody) // default title shape
		if strings.TrimSpace(s.Body) != "" {
			bodyText := gopptx.DecodeTextBody{
				Paragraph: []gopptx.DecodeParagraph{
					{
						Runs: []gopptx.DecodeRuns{{Text: s.Body}},
					},
				},
			}
			_ = f.SetShapeTextBody(slideID, 8, bodyText) // default body shape
		}
	}
	return f.SaveAs(dstPath)
}

func containsAny(s string, keys ...string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	for _, k := range keys {
		if strings.Contains(s, k) {
			return true
		}
	}
	return false
}

type textShapeGeo struct {
	ID int
	X  int64
	Y  int64
}

type textShapeMeta struct {
	ID              int
	X               int64
	Y               int64
	PlaceholderType string
	Name            string
}

var (
	reShapeBlock = regexp.MustCompile(`(?s)<p:sp\b.*?</p:sp>`)
	reShapeID    = regexp.MustCompile(`<p:cNvPr\b[^>]*\bid="(\d+)"`)
	reShapeName  = regexp.MustCompile(`<p:cNvPr\b[^>]*\bname="([^"]*)"`)
	reShapeOff   = regexp.MustCompile(`<a:off\b[^>]*\bx="(\d+)"[^>]*\by="(\d+)"`)
	reShapePH    = regexp.MustCompile(`<p:ph\b[^>]*\btype="([^"]+)"`)
	reParagraph  = regexp.MustCompile(`(?s)<a:p\b.*?</a:p>`)
)

func readSlideXMLFromPPTX(srcPPTX string, slideIndex int) ([]byte, error) {
	r, err := zip.OpenReader(srcPPTX)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	target := fmt.Sprintf("ppt/slides/slide%d.xml", slideIndex)
	for _, f := range r.File {
		if strings.EqualFold(filepath.ToSlash(f.Name), target) {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(rc)
		}
	}
	return nil, fmt.Errorf("slide xml not found: %s", target)
}

// orderedTextShapeIDsByGeometry 按模板中 shape 几何顺序（Y, X, ID）输出文本 shape id。
// 仅返回 allowed 中已有的文本 shape（由 gopptx.GetShapes 提供可写 shape 集合）。
func orderedTextShapeIDsByGeometry(srcPPTX string, slideIndex int, allowed map[int]struct{}) []int {
	raw, err := readSlideXMLFromPPTX(srcPPTX, slideIndex)
	if err != nil {
		return nil
	}
	blocks := reShapeBlock.FindAll(raw, -1)
	if len(blocks) == 0 {
		return nil
	}
	out := make([]textShapeGeo, 0, len(blocks))
	for _, b := range blocks {
		blk := string(b)
		if !strings.Contains(blk, "<p:txBody") {
			continue
		}
		idm := reShapeID.FindStringSubmatch(blk)
		if len(idm) < 2 {
			continue
		}
		id, err := strconv.Atoi(strings.TrimSpace(idm[1]))
		if err != nil || id <= 0 {
			continue
		}
		if _, ok := allowed[id]; !ok {
			continue
		}
		x := int64(1 << 62)
		y := int64(1 << 62)
		if om := reShapeOff.FindStringSubmatch(blk); len(om) >= 3 {
			if xv, e := strconv.ParseInt(strings.TrimSpace(om[1]), 10, 64); e == nil {
				x = xv
			}
			if yv, e := strconv.ParseInt(strings.TrimSpace(om[2]), 10, 64); e == nil {
				y = yv
			}
		}
		out = append(out, textShapeGeo{ID: id, X: x, Y: y})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Y != out[j].Y {
			return out[i].Y < out[j].Y
		}
		if out[i].X != out[j].X {
			return out[i].X < out[j].X
		}
		return out[i].ID < out[j].ID
	})
	ids := make([]int, 0, len(out))
	for _, it := range out {
		ids = append(ids, it.ID)
	}
	return ids
}

func extractTextShapeMetas(srcPPTX string, slideIndex int, allowed map[int]struct{}) []textShapeMeta {
	raw, err := readSlideXMLFromPPTX(srcPPTX, slideIndex)
	if err != nil {
		return nil
	}
	blocks := reShapeBlock.FindAll(raw, -1)
	if len(blocks) == 0 {
		return nil
	}
	out := make([]textShapeMeta, 0, len(blocks))
	for _, b := range blocks {
		blk := string(b)
		if !strings.Contains(blk, "<p:txBody") {
			continue
		}
		idm := reShapeID.FindStringSubmatch(blk)
		if len(idm) < 2 {
			continue
		}
		id, err := strconv.Atoi(strings.TrimSpace(idm[1]))
		if err != nil || id <= 0 {
			continue
		}
		if _, ok := allowed[id]; !ok {
			continue
		}
		x := int64(1 << 62)
		y := int64(1 << 62)
		if om := reShapeOff.FindStringSubmatch(blk); len(om) >= 3 {
			if xv, e := strconv.ParseInt(strings.TrimSpace(om[1]), 10, 64); e == nil {
				x = xv
			}
			if yv, e := strconv.ParseInt(strings.TrimSpace(om[2]), 10, 64); e == nil {
				y = yv
			}
		}
		name := ""
		if nm := reShapeName.FindStringSubmatch(blk); len(nm) >= 2 {
			name = strings.ToLower(strings.TrimSpace(nm[1]))
		}
		phType := ""
		if pm := reShapePH.FindStringSubmatch(blk); len(pm) >= 2 {
			phType = strings.ToLower(strings.TrimSpace(pm[1]))
		}
		out = append(out, textShapeMeta{
			ID:              id,
			X:               x,
			Y:               y,
			PlaceholderType: phType,
			Name:            name,
		})
	}
	return out
}

// orderedTextShapeIDsSemantic 优先 title/ctrTitle 语义槽位，其余按几何顺序。
func orderedTextShapeIDsSemantic(srcPPTX string, slideIndex int, allowed map[int]struct{}) []int {
	metas := extractTextShapeMetas(srcPPTX, slideIndex, allowed)
	if len(metas) == 0 {
		return orderedTextShapeIDsByGeometry(srcPPTX, slideIndex, allowed)
	}
	sort.Slice(metas, func(i, j int) bool {
		if metas[i].Y != metas[j].Y {
			return metas[i].Y < metas[j].Y
		}
		if metas[i].X != metas[j].X {
			return metas[i].X < metas[j].X
		}
		return metas[i].ID < metas[j].ID
	})
	titleIDs := make([]int, 0, 2)
	restIDs := make([]int, 0, len(metas))
	seen := map[int]struct{}{}
	for _, m := range metas {
		if containsAny(m.PlaceholderType, "title", "ctrtitle") || containsAny(m.Name, "title", "标题", "ctrtitle") {
			titleIDs = append(titleIDs, m.ID)
			seen[m.ID] = struct{}{}
		}
	}
	for _, m := range metas {
		if _, ok := seen[m.ID]; ok {
			continue
		}
		restIDs = append(restIDs, m.ID)
	}
	return append(titleIDs, restIDs...)
}

func xmlEscapeText(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func replaceShapeTextPreserveRuns(slideXML []byte, shapeID int, text string) ([]byte, bool) {
	blockRe := regexp.MustCompile(fmt.Sprintf(`(?s)<p:sp\b.*?<p:cNvPr\b[^>]*\bid="%d"[^>]*>.*?</p:sp>`, shapeID))
	loc := blockRe.FindIndex(slideXML)
	if len(loc) != 2 {
		return slideXML, false
	}
	block := string(slideXML[loc[0]:loc[1]])
	tRe := regexp.MustCompile(`(?s)<a:t>.*?</a:t>`)
	matches := tRe.FindAllStringIndex(block, -1)
	if len(matches) == 0 {
		return slideXML, false
	}
	lines := strings.Split(strings.ReplaceAll(strings.TrimSpace(text), "\r\n", "\n"), "\n")
	clean := make([]string, 0, len(lines))
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if ln != "" {
			clean = append(clean, ln)
		}
	}
	if len(clean) == 0 {
		clean = []string{""}
	}
	// 优先按段落映射：保留每段样式/项目符号，只替换段内文本 run。
	paras := reParagraph.FindAllString(block, -1)
	if len(paras) > 0 {
		assign := make([]string, len(paras))
		if len(paras) == 1 {
			assign[0] = strings.Join(clean, " ")
		} else {
			cur := 0
			for i := 0; i < len(paras)-1; i++ {
				if cur < len(clean) {
					assign[i] = clean[cur]
					cur++
				}
			}
			if cur < len(clean) {
				assign[len(paras)-1] = strings.Join(clean[cur:], " ")
			} else {
				assign[len(paras)-1] = ""
			}
		}
		updatedParas := make([]string, 0, len(paras))
		for i, p := range paras {
			pm := tRe.FindAllStringIndex(p, -1)
			if len(pm) == 0 {
				updatedParas = append(updatedParas, p)
				continue
			}
			target := assign[i]
			runCur := 0
			np := tRe.ReplaceAllStringFunc(p, func(_ string) string {
				if runCur == 0 {
					runCur++
					return "<a:t>" + xmlEscapeText(target) + "</a:t>"
				}
				runCur++
				return "<a:t></a:t>"
			})
			updatedParas = append(updatedParas, np)
		}
		// 将更新后的段落依次回写到 shape block。
		pi := 0
		block = reParagraph.ReplaceAllStringFunc(block, func(_ string) string {
			if pi >= len(updatedParas) {
				return ""
			}
			v := updatedParas[pi]
			pi++
			return v
		})
	} else {
		// 无段落结构时回退到 run 级替换。
		last := len(matches) - 1
		cur := 0
		block = tRe.ReplaceAllStringFunc(block, func(_ string) string {
			var val string
			switch {
			case cur < len(clean)-1 && cur < last:
				val = clean[cur]
			case cur == last:
				if len(clean) <= last+1 {
					val = clean[minInt(cur, len(clean)-1)]
				} else {
					val = strings.Join(clean[cur:], " ")
				}
			default:
				val = ""
			}
			cur++
			return "<a:t>" + xmlEscapeText(val) + "</a:t>"
		})
	}
	out := append([]byte{}, slideXML[:loc[0]]...)
	out = append(out, []byte(block)...)
	out = append(out, slideXML[loc[1]:]...)
	return out, true
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func applyTextAssignmentsByXML(srcPPTX, dstPPTX string, assigns []textAssignment) error {
	if len(assigns) == 0 {
		return RoundTrip(srcPPTX, dstPPTX)
	}
	entries, err := readZipEntries(srcPPTX)
	if err != nil {
		return err
	}
	for _, a := range assigns {
		if a.SlideIndex <= 0 || a.ShapeID <= 0 {
			continue
		}
		p := normalizeZipPath(fmt.Sprintf("ppt/slides/slide%d.xml", a.SlideIndex))
		raw, ok := entries[p]
		if !ok || len(raw) == 0 {
			continue
		}
		updated, ok := replaceShapeTextPreserveRuns(raw, a.ShapeID, a.Text)
		if !ok {
			continue
		}
		entries[p] = updated
	}
	return writeZipEntries(dstPPTX, entries)
}

// BuildElementPlansFromContentPlan 从 /generate-pres 的 content_plan（any）提取结构化元素，供语义写回。
func BuildElementPlansFromContentPlan(contentPlan any) []SlideElementPlan {
	// 先尝试 []map[string]any
	arr, ok := contentPlan.([]map[string]any)
	if !ok {
		raw, ok2 := contentPlan.([]any)
		if !ok2 {
			return nil
		}
		arr = make([]map[string]any, 0, len(raw))
		for _, v := range raw {
			if m, ok := v.(map[string]any); ok {
				arr = append(arr, m)
			}
		}
	}
	out := make([]SlideElementPlan, 0, len(arr))
	for i, it := range arr {
		idx := i + 1
		if v, ok := it["slide_index"].(float64); ok && int(v) > 0 {
			idx = int(v)
		}
		elemsRaw, _ := it["elements"].([]any)
		elems := make([]ElementPlan, 0, len(elemsRaw))
		for _, e := range elemsRaw {
			em, ok := e.(map[string]any)
			if !ok {
				continue
			}
			name := strings.TrimSpace(fmt.Sprint(em["name"]))
			dsRaw, _ := em["data"].([]any)
			ds := make([]string, 0, len(dsRaw))
			for _, d := range dsRaw {
				ds = append(ds, strings.TrimSpace(fmt.Sprint(d)))
			}
			elems = append(elems, ElementPlan{Name: name, Data: ds})
		}
		out = append(out, SlideElementPlan{SlideIndex: idx, Elements: elems})
	}
	return out
}

func maybeImageValue(s string) bool {
	x := strings.ToLower(strings.TrimSpace(s))
	if x == "" {
		return false
	}
	if strings.HasPrefix(x, "http://") || strings.HasPrefix(x, "https://") || strings.HasPrefix(x, "data:image/") {
		return true
	}
	for _, suf := range []string{".png", ".jpg", ".jpeg", ".webp", ".gif", ".bmp", ".svg"} {
		if strings.HasSuffix(x, suf) {
			return true
		}
	}
	return containsAny(x, "image", "img", "picture", "logo")
}

func makeTextBody(text string) gopptx.DecodeTextBody {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	paras := make([]gopptx.DecodeParagraph, 0, len(lines))
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		paras = append(paras, gopptx.DecodeParagraph{
			Runs: []gopptx.DecodeRuns{{Text: ln}},
		})
	}
	if len(paras) == 0 {
		paras = append(paras, gopptx.DecodeParagraph{
			Runs: []gopptx.DecodeRuns{{Text: ""}},
		})
	}
	return gopptx.DecodeTextBody{Paragraph: paras}
}

func chooseTitleAndBodies(pl SlideElementPlan) (string, []string) {
	title := ""
	bodies := make([]string, 0, len(pl.Elements))
	for _, e := range pl.Elements {
		if len(e.Data) == 0 {
			continue
		}
		val := joinLines(e.Data)
		if val == "" {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(e.Name))
		isTitle := containsAny(name, "title", "header", "opening", "main title", "section subtitle")
		if isTitle && title == "" {
			// 标题优先使用首句，避免过长。
			if p := strings.Split(val, "\n"); len(p) > 0 {
				title = strings.TrimSpace(p[0])
			} else {
				title = val
			}
			if strings.Contains(val, "\n") {
				rest := strings.TrimSpace(strings.Join(strings.Split(val, "\n")[1:], "\n"))
				if rest != "" {
					bodies = append(bodies, rest)
				}
			}
			continue
		}
		if e.Name != "" {
			bodies = append(bodies, e.Name+": "+val)
		} else {
			bodies = append(bodies, val)
		}
	}
	if title == "" && len(bodies) > 0 {
		title = bodies[0]
		bodies = bodies[1:]
	}
	if title == "" {
		title = "Slide"
	}
	return title, bodies
}

// ApplyContentSlidesToTemplatePPTX 在既有模板 PPT 上按页写入文本（标题/正文）。
// 策略：每页按 shape id 升序选择前两个可写文本 shape，分别写 Title 与 Body。
func ApplyContentSlidesToTemplatePPTX(srcPPTX, dstPPTX string, slides []ContentSlide) error {
	if len(slides) == 0 {
		return fmt.Errorf("slides empty")
	}
	f, err := gopptx.OpenFile(srcPPTX)
	if err != nil {
		return fmt.Errorf("open pptx: %w", err)
	}
	defer f.Close()

	slideIDs := f.GetSlideList()
	if len(slideIDs) == 0 {
		return fmt.Errorf("template has no slides")
	}
	limit := len(slides)
	if limit > len(slideIDs) {
		limit = len(slideIDs)
	}
	assigns := make([]textAssignment, 0, limit*2)
	for i := 0; i < limit; i++ {
		sid := slideIDs[i]
		shapes, err := f.GetShapes(sid)
		if err != nil {
			continue
		}
		textShapeIDs := make([]int, 0, len(shapes))
		allowed := make(map[int]struct{}, len(shapes))
		for _, s := range shapes {
			if s.TextBody == nil || s.NonVisualShapeProperties == nil || s.NonVisualShapeProperties.CommonNonVisualProperties == nil {
				continue
			}
			id := s.NonVisualShapeProperties.CommonNonVisualProperties.ID
			textShapeIDs = append(textShapeIDs, id)
			allowed[id] = struct{}{}
		}
		if geo := orderedTextShapeIDsSemantic(srcPPTX, i+1, allowed); len(geo) > 0 {
			textShapeIDs = geo
		} else {
			sort.Ints(textShapeIDs)
		}
		if len(textShapeIDs) == 0 {
			continue
		}
		assigns = append(assigns, textAssignment{
			SlideIndex: i + 1,
			ShapeID:    textShapeIDs[0],
			Text:       slides[i].Title,
		})
		if len(textShapeIDs) > 1 && strings.TrimSpace(slides[i].Body) != "" {
			assigns = append(assigns, textAssignment{
				SlideIndex: i + 1,
				ShapeID:    textShapeIDs[1],
				Text:       slides[i].Body,
			})
		}
	}
	if err := os.MkdirAll(filepath.Dir(dstPPTX), 0o755); err != nil {
		return err
	}
	_ = f
	return applyTextAssignmentsByXML(srcPPTX, dstPPTX, assigns)
}

// ApplyElementPlansToTemplatePPTX 按 element 名称做语义写回：
// - title/header 类写入第一个文本 shape
// - 其余元素按顺序分配到后续文本 shape（溢出时合并到最后一个）
func ApplyElementPlansToTemplatePPTX(srcPPTX, dstPPTX string, plans []SlideElementPlan) error {
	if len(plans) == 0 {
		return fmt.Errorf("plans empty")
	}
	f, err := gopptx.OpenFile(srcPPTX)
	if err != nil {
		return fmt.Errorf("open pptx: %w", err)
	}
	defer f.Close()

	slideIDs := f.GetSlideList()
	if len(slideIDs) == 0 {
		return fmt.Errorf("template has no slides")
	}
	limit := len(plans)
	if limit > len(slideIDs) {
		limit = len(slideIDs)
	}
	assigns := make([]textAssignment, 0, limit*4)
	for i := 0; i < limit; i++ {
		sid := slideIDs[i]
		shapes, err := f.GetShapes(sid)
		if err != nil {
			continue
		}
		textShapeIDs := make([]int, 0, len(shapes))
		allowed := make(map[int]struct{}, len(shapes))
		for _, s := range shapes {
			if s.TextBody == nil || s.NonVisualShapeProperties == nil || s.NonVisualShapeProperties.CommonNonVisualProperties == nil {
				continue
			}
			id := s.NonVisualShapeProperties.CommonNonVisualProperties.ID
			textShapeIDs = append(textShapeIDs, id)
			allowed[id] = struct{}{}
		}
		if geo := orderedTextShapeIDsSemantic(srcPPTX, i+1, allowed); len(geo) > 0 {
			textShapeIDs = geo
		} else {
			sort.Ints(textShapeIDs)
		}
		if len(textShapeIDs) == 0 {
			continue
		}

		title, bodies := chooseTitleAndBodies(plans[i])
		assigns = append(assigns, textAssignment{
			SlideIndex: i + 1,
			ShapeID:    textShapeIDs[0],
			Text:       title,
		})

		if len(textShapeIDs) <= 1 || len(bodies) == 0 {
			continue
		}
		bodySlots := textShapeIDs[1:]
		if len(bodies) <= len(bodySlots) {
			for k, txt := range bodies {
				assigns = append(assigns, textAssignment{
					SlideIndex: i + 1,
					ShapeID:    bodySlots[k],
					Text:       txt,
				})
			}
			continue
		}
		// body 超出 shape 数时，最后一个 shape 合并剩余文本。
		for k := 0; k < len(bodySlots)-1; k++ {
			assigns = append(assigns, textAssignment{
				SlideIndex: i + 1,
				ShapeID:    bodySlots[k],
				Text:       bodies[k],
			})
		}
		rest := strings.Join(bodies[len(bodySlots)-1:], "\n\n")
		assigns = append(assigns, textAssignment{
			SlideIndex: i + 1,
			ShapeID:    bodySlots[len(bodySlots)-1],
			Text:       rest,
		})
	}
	if err := os.MkdirAll(filepath.Dir(dstPPTX), 0o755); err != nil {
		return err
	}
	_ = f
	return applyTextAssignmentsByXML(srcPPTX, dstPPTX, assigns)
}

// BuildImageApplyPlan 识别每页图片候选值，并扫描模板中可疑图片槽位（非文本 shape）。
// 当前 gopptx 图片替换 API 未稳定暴露时，先输出该计划供后续替换器使用。
func BuildImageApplyPlan(srcPPTX string, plans []SlideElementPlan) ([]ImageApplyPlan, error) {
	if len(plans) == 0 {
		return nil, nil
	}
	f, err := gopptx.OpenFile(srcPPTX)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	slideIDs := f.GetSlideList()
	limit := len(plans)
	if limit > len(slideIDs) {
		limit = len(slideIDs)
	}
	out := make([]ImageApplyPlan, 0, limit)
	for i := 0; i < limit; i++ {
		p := plans[i]
		imgVals := make([]string, 0)
		for _, e := range p.Elements {
			n := strings.ToLower(strings.TrimSpace(e.Name))
			isImgElem := containsAny(n, "image", "img", "logo", "picture", "photo")
			for _, d := range e.Data {
				if isImgElem || maybeImageValue(d) {
					imgVals = append(imgVals, strings.TrimSpace(d))
				}
			}
		}
		shapes, _ := f.GetShapes(slideIDs[i])
		cands := make([]int, 0)
		for _, s := range shapes {
			if s.NonVisualShapeProperties == nil || s.NonVisualShapeProperties.CommonNonVisualProperties == nil {
				continue
			}
			// 近似：非文本 shape 作为图片槽候选
			if s.TextBody == nil {
				cands = append(cands, s.NonVisualShapeProperties.CommonNonVisualProperties.ID)
			}
		}
		out = append(out, ImageApplyPlan{
			SlideIndex:       p.SlideIndex,
			ImageValues:      imgVals,
			CandidateSlotIDs: cands,
			Note:             "pending image replacement implementation with gopptx",
		})
	}
	return out, nil
}

