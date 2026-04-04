package orchestrator

import (
	"bytes"
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"

	"educationagent/pptagentgo/internal/fullgen/domain"
	"educationagent/pptagentgo/pkg/infer"
	"github.com/kenny-not-dead/gopptx"
)

type stage1SlideMeta struct {
	Index      int
	Texts      []string
	TextCount  int
	ImageCount int
}

type Stage1LiveResult struct {
	Induction       json.RawMessage `json:"induction"`
	CategoryCluster map[string][]int `json:"category_cluster,omitempty"`
	LayoutGroups    map[string][]int `json:"layout_groups,omitempty"`
}

type stage1EmbeddingResponse struct {
	Code    int `json:"code"`
	Message string `json:"message"`
	Data struct {
		Vector []float64 `json:"vector"`
	} `json:"data"`
	Vector []float64 `json:"vector"`
}

func readSlideXML(srcPPTX string, slideIndex int) ([]byte, error) {
	r, err := zip.OpenReader(srcPPTX)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	target := fmt.Sprintf("ppt/slides/slide%d.xml", slideIndex)
	for _, f := range r.File {
		if strings.EqualFold(strings.ReplaceAll(f.Name, "\\", "/"), target) {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(rc)
		}
	}
	return nil, fmt.Errorf("missing %s", target)
}

func extractTextsFromSlideXML(raw []byte) []string {
	s := string(raw)
	out := make([]string, 0, 16)
	for {
		i := strings.Index(s, "<a:t>")
		if i < 0 {
			break
		}
		j := strings.Index(s[i:], "</a:t>")
		if j < 0 {
			break
		}
		txt := strings.TrimSpace(s[i+len("<a:t>") : i+j])
		if txt != "" {
			out = append(out, txt)
		}
		s = s[i+j+len("</a:t>"):]
	}
	return out
}

func countPicsInSlideXML(raw []byte) int {
	return strings.Count(string(raw), "<p:pic")
}

func buildSlideMetas(srcPPTX string, slideCount int) []stage1SlideMeta {
	out := make([]stage1SlideMeta, 0, slideCount)
	for i := 1; i <= slideCount; i++ {
		raw, err := readSlideXML(srcPPTX, i)
		if err != nil {
			out = append(out, stage1SlideMeta{Index: i})
			continue
		}
		txts := extractTextsFromSlideXML(raw)
		out = append(out, stage1SlideMeta{
			Index:      i,
			Texts:      txts,
			TextCount:  len(txts),
			ImageCount: countPicsInSlideXML(raw),
		})
	}
	return out
}

func buildCategoryInput(metas []stage1SlideMeta) string {
	var b strings.Builder
	for _, m := range metas {
		preview := strings.Join(m.Texts, " | ")
		if len([]rune(preview)) > 180 {
			preview = string([]rune(preview)[:180]) + "..."
		}
		b.WriteString(fmt.Sprintf("Slide %d: texts=%d images=%d content=%s\n", m.Index, m.TextCount, m.ImageCount, preview))
	}
	return strings.TrimSpace(b.String())
}

func parseCategoryJSON(raw string) map[string][]int {
	s := strings.TrimSpace(raw)
	if strings.HasPrefix(s, "```") {
		lines := strings.Split(s, "\n")
		if len(lines) > 0 {
			lines = lines[1:]
		}
		for len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[len(lines)-1]), "```") {
			lines = lines[:len(lines)-1]
		}
		s = strings.TrimSpace(strings.Join(lines, "\n"))
	}
	out := map[string][]int{}
	_ = json.Unmarshal([]byte(s), &out)
	return out
}

func findRepresentativeSlides(metas []stage1SlideMeta, contentSet map[int]struct{}) map[string][]int {
	type group struct {
		slides     []int
		templateID int
		maxShapes  int
	}
	groups := map[string]*group{}
	for _, m := range metas {
		if len(contentSet) > 0 {
			if _, ok := contentSet[m.Index]; !ok {
				continue
			}
		}
		key := fmt.Sprintf("layout_t%d_i%d:%s", min(m.TextCount, 6), min(m.ImageCount, 4), func() string {
			if m.ImageCount > 0 {
				return "image"
			}
			return "text"
		}())
		g, ok := groups[key]
		if !ok {
			g = &group{}
			groups[key] = g
		}
		g.slides = append(g.slides, m.Index)
		score := m.TextCount + m.ImageCount
		if score >= g.maxShapes {
			g.maxShapes = score
			g.templateID = m.Index
		}
	}
	out := map[string][]int{}
	for k, g := range groups {
		if len(g.slides) == 0 {
			continue
		}
		sort.Ints(g.slides)
		out[k] = g.slides
	}
	return out
}

func stage1EmbedURL() string {
	return strings.TrimSpace(os.Getenv("PPTAGENT_STAGE1_EMBED_URL"))
}

func stage1ClusterThreshold() float64 {
	v := strings.TrimSpace(os.Getenv("PPTAGENT_STAGE1_CLUSTER_THRESHOLD"))
	if v == "" {
		return 0.86
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || f <= 0 || f > 1 {
		return 0.86
	}
	return f
}

func embedText(ctx context.Context, url string, text string) ([]float64, error) {
	body, _ := json.Marshal(map[string]any{"text": text})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("embed status %d", resp.StatusCode)
	}
	var out stage1EmbeddingResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	if len(out.Data.Vector) > 0 {
		return out.Data.Vector, nil
	}
	if len(out.Vector) > 0 {
		return out.Vector, nil
	}
	return nil, fmt.Errorf("empty vector")
}

func cosine(a, b []float64) float64 {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

func clusterByEmbedding(
	ctx context.Context,
	metas []stage1SlideMeta,
	contentSet map[int]struct{},
	url string,
	threshold float64,
) map[string][]int {
	type item struct {
		Idx   int
		Vec   []float64
		Score string
	}
	items := make([]item, 0, len(contentSet))
	for _, m := range metas {
		if _, ok := contentSet[m.Index]; !ok {
			continue
		}
		text := strings.Join(m.Texts, "\n")
		if strings.TrimSpace(text) == "" {
			text = fmt.Sprintf("slide=%d text=%d image=%d", m.Index, m.TextCount, m.ImageCount)
		}
		vec, err := embedText(ctx, url, text)
		if err != nil || len(vec) == 0 {
			continue
		}
		score := "text"
		if m.ImageCount > 0 {
			score = "image"
		}
		items = append(items, item{Idx: m.Index, Vec: vec, Score: score})
	}
	if len(items) == 0 {
		return nil
	}
	parent := make([]int, len(items))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(x int) int {
		if parent[x] != x {
			parent[x] = find(parent[x])
		}
		return parent[x]
	}
	union := func(a, b int) {
		ra, rb := find(a), find(b)
		if ra != rb {
			parent[rb] = ra
		}
	}
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			if items[i].Score != items[j].Score {
				continue
			}
			if cosine(items[i].Vec, items[j].Vec) >= threshold {
				union(i, j)
			}
		}
	}
	groups := map[int][]item{}
	for i := range items {
		r := find(i)
		groups[r] = append(groups[r], items[i])
	}
	out := map[string][]int{}
	idx := 1
	for _, g := range groups {
		if len(g) == 0 {
			continue
		}
		sort.Slice(g, func(i, j int) bool { return g[i].Idx < g[j].Idx })
		key := fmt.Sprintf("cluster_%02d:%s", idx, g[0].Score)
		arr := make([]int, 0, len(g))
		for _, it := range g {
			arr = append(arr, it.Idx)
		}
		out[key] = arr
		idx++
	}
	return out
}

func schemaFromRoleOrFallback(ctx context.Context, o *Orchestrator, slideIdx int, meta stage1SlideMeta) map[string]any {
	slideHTML := "<div>\n"
	for _, t := range meta.Texts {
		slideHTML += "<p>" + t + "</p>\n"
	}
	for i := 0; i < max(1, meta.ImageCount); i++ {
		slideHTML += `<img alt="image" src="image.png" />` + "\n"
	}
	slideHTML += "</div>"
	raw, err := o.RunRole(ctx, "schema_extractor", map[string]any{
		"slide_idx": slideIdx,
		"slide":     slideHTML,
	})
	if err == nil {
		var top map[string]any
		if e := json.Unmarshal([]byte(raw), &top); e == nil {
			if _, ok := top["elements"]; ok {
				return top
			}
		}
	}
	return map[string]any{
		"elements": []map[string]any{
			{"name": "main title", "type": "text", "data": []string{"title"}},
			{"name": "body", "type": "text", "data": []string{"content"}},
			{"name": "main image", "type": "image", "data": []string{"image"}},
		},
	}
}

func inferLayoutName(ctx context.Context, o *Orchestrator, key string, meta stage1SlideMeta) string {
	if o == nil || o.Infer == nil {
		return key
	}
	preview := strings.Join(meta.Texts, " | ")
	if len([]rune(preview)) > 240 {
		preview = string([]rune(preview)[:240]) + "..."
	}
	prompt := fmt.Sprintf(
		"Given a representative slide summary, return JSON with concise layout_name in snake_case and content_type in {text,image}. summary=%q text_count=%d image_count=%d",
		preview, meta.TextCount, meta.ImageCount,
	)
	raw, err := o.Infer.Complete(ctx, prompt, "You name PowerPoint layout patterns.", &infer.Options{JSONMode: true})
	if err != nil {
		return key
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		return key
	}
	name := strings.ToLower(strings.TrimSpace(fmt.Sprint(obj["layout_name"])))
	ctype := strings.ToLower(strings.TrimSpace(fmt.Sprint(obj["content_type"])))
	name = strings.ReplaceAll(name, " ", "_")
	name = strings.ReplaceAll(name, "-", "_")
	if name == "" {
		return key
	}
	if ctype != "image" {
		ctype = "text"
	}
	return name + ":" + ctype
}

// RunStage1Live 在推理阶段基于 source.pptx 运行更高保真 Stage I（LLM+启发式兜底）。
func (o *Orchestrator) RunStage1Live(ctx context.Context, bundle *domain.TemplateBundle) (*Stage1LiveResult, error) {
	if bundle == nil || strings.TrimSpace(bundle.SourcePPTX) == "" {
		return nil, fmt.Errorf("bundle/source.pptx empty")
	}
	f, err := gopptx.OpenFile(bundle.SourcePPTX)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	slideIDs := f.GetSlideList()
	if len(slideIDs) == 0 {
		return nil, fmt.Errorf("template has no slides")
	}
	metas := buildSlideMetas(bundle.SourcePPTX, len(slideIDs))

	functionalCluster := map[string][]int{}
	if o != nil && o.Infer != nil {
		prompt := `You are an expert presentation analyst specializing in identifying structural slides:
- opening
- table of contents
- section outline
- ending
Return strict JSON object: {"opening":[...], "table of contents":[...], "section outline":[...], "ending":[...]}.
Only include keys that exist.`
		raw, e := o.Infer.Complete(ctx, buildCategoryInput(metas), prompt, &infer.Options{JSONMode: true})
		if e == nil {
			functionalCluster = parseCategoryJSON(raw)
		}
	}
	if len(functionalCluster) == 0 {
		// 启发式兜底：首尾页视为 opening/ending。
		functionalCluster = map[string][]int{}
		if len(slideIDs) >= 1 {
			functionalCluster["opening"] = []int{1}
		}
		if len(slideIDs) >= 2 {
			functionalCluster["ending"] = []int{len(slideIDs)}
		}
	}

	contentSet := map[int]struct{}{}
	functionSet := map[int]struct{}{}
	for _, arr := range functionalCluster {
		for _, idx := range arr {
			functionSet[idx] = struct{}{}
		}
	}
	for i := 1; i <= len(slideIDs); i++ {
		if _, ok := functionSet[i]; !ok {
			contentSet[i] = struct{}{}
		}
	}
	rawLayoutGroups := map[string][]int{}
	if u := stage1EmbedURL(); u != "" {
		rawLayoutGroups = clusterByEmbedding(ctx, metas, contentSet, u, stage1ClusterThreshold())
	}
	if len(rawLayoutGroups) == 0 {
		rawLayoutGroups = findRepresentativeSlides(metas, contentSet)
	}

	functionalKeys := make([]string, 0, len(functionalCluster))
	for k := range functionalCluster {
		functionalKeys = append(functionalKeys, k)
	}
	sort.Strings(functionalKeys)

	keys := make([]string, 0, len(rawLayoutGroups))
	for k := range rawLayoutGroups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	layoutGroups := map[string][]int{}

	top := map[string]any{
		"language":        map[string]any{"lid": "en"},
		"functional_keys": functionalKeys,
	}
	for _, fk := range functionalKeys {
		arr := functionalCluster[fk]
		if len(arr) == 0 {
			continue
		}
		sort.Ints(arr)
		top[fk] = map[string]any{
			"template_id": arr[0],
			"slides":      arr,
			"content_schema": map[string]any{
				"elements": []map[string]any{
					{"name": "title", "type": "text", "data": []string{"title"}},
				},
			},
		}
	}
	for _, k := range keys {
		slides := rawLayoutGroups[k]
		templateID := slides[0]
		for _, idx := range slides {
			if idx < templateID {
				templateID = idx
			}
		}
		var meta stage1SlideMeta
		for _, m := range metas {
			if m.Index == templateID {
				meta = m
				break
			}
		}
		llmKey := inferLayoutName(ctx, o, k, meta)
		if _, exists := layoutGroups[llmKey]; exists {
			llmKey = llmKey + "_" + strconv.Itoa(templateID)
		}
		layoutGroups[llmKey] = slides
		schema := schemaFromRoleOrFallback(ctx, o, templateID, meta)
		top[llmKey] = map[string]any{
			"template_id":   templateID,
			"slides":        slides,
			"content_schema": schema,
		}
	}
	raw, err := json.Marshal(top)
	if err != nil {
		return nil, err
	}
	return &Stage1LiveResult{
		Induction:       raw,
		CategoryCluster: functionalCluster,
		LayoutGroups:    layoutGroups,
	}, nil
}

