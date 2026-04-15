package model

// Requirements represents the collected user requirements.
type Requirements struct {
	Topic      *string `json:"topic"`
	Style      *string `json:"style"`
	TotalPages *int    `json:"total_pages"`
	Audience   *string `json:"audience"`
}

// MissingFields returns a list of field names that are still nil.
func (r Requirements) MissingFields() []string {
	var missing []string
	if r.Topic == nil {
		missing = append(missing, "topic")
	}
	if r.Style == nil {
		missing = append(missing, "style")
	}
	if r.TotalPages == nil {
		missing = append(missing, "total_pages")
	}
	if r.Audience == nil {
		missing = append(missing, "audience")
	}
	return missing
}

// IsComplete returns true when all four fields are set.
func (r Requirements) IsComplete() bool {
	return r.Topic != nil && r.Style != nil && r.TotalPages != nil && r.Audience != nil
}

// ToMap converts requirements to a map[string]any for JSON responses.
func (r Requirements) ToMap() map[string]any {
	m := make(map[string]any, 4)
	if r.Topic != nil {
		m["topic"] = *r.Topic
	}
	if r.Style != nil {
		m["style"] = *r.Style
	}
	if r.TotalPages != nil {
		m["total_pages"] = *r.TotalPages
	}
	if r.Audience != nil {
		m["audience"] = *r.Audience
	}
	return m
}

// UniformResponse is the standard JSON envelope for all API responses.
type UniformResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data"`
}

// Request/response DTOs

type UpdateRequirementsRequest struct {
	From         string         `json:"from"`
	To           string         `json:"to"`
	Requirements map[string]any `json:"requirements"`
}

type UpdateRequirementsData struct {
	MissingFields []string `json:"missing_fields"`
}

type RequireConfirmRequest struct {
	From         string         `json:"from"`
	To           string         `json:"to"`
	Requirements map[string]any `json:"requirements"`
}

type SendToPPTAgentRequest struct {
	From string `json:"from"`
	To   string `json:"to"`
	Data string `json:"data"`
}

type FetchFromPPTMessageQueueRequest struct {
	From string `json:"from"`
}

type StartConversationRequest struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type SendToVoiceAgentRequest struct {
	From string `json:"from"`
	To   string `json:"to"`
	Data string `json:"data"`
}

type QueryChunksRequest struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Query string `json:"query"`
}

type Chunk struct {
	ChunkID string `json:"chunk_id"`
	Content string `json:"content"`
}

type QueryChunksData struct {
	Chunks []Chunk `json:"chunks"`
	Total  int     `json:"total"`
}

type SearchQueryRequest struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Query string `json:"query"`
}
