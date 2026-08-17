package model

// UniformResponse is the common JSON envelope returned by most endpoints.
type UniformResponse[T any] struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    T      `json:"data"`
}

// UpdateRequirementsData is returned by POST /api/v1/update_requirements.
type UpdateRequirementsData struct {
	MissingFields []string `json:"missing_fields"`
}

// SSEChunk is a server-sent event emitted by the voice turn endpoints.
type SSEChunk struct {
	Type    string `json:"type"`
	Text    string `json:"text,omitempty"`
	Payload string `json:"payload,omitempty"`
}

// VadStartData is returned by POST /api/v1/voice/vad_start.
type VadStartData struct {
	Interrupt bool `json:"interrupt"`
}

// Requirements holds the four fields collected in Phase 1.
type Requirements struct {
	Topic      *string `json:"topic"`
	Style      *string `json:"style"`
	TotalPages *int    `json:"total_pages"`
	Audience   *string `json:"audience"`
}

// IsComplete reports whether all required fields are present.
func (r Requirements) IsComplete() bool {
	return r.Topic != nil && *r.Topic != "" &&
		r.Style != nil && *r.Style != "" &&
		r.TotalPages != nil && *r.TotalPages > 0 &&
		r.Audience != nil && *r.Audience != ""
}

// MissingFields returns the names of fields that are still missing.
func (r Requirements) MissingFields() []string {
	var missing []string
	if r.Topic == nil || *r.Topic == "" {
		missing = append(missing, "topic")
	}
	if r.Style == nil || *r.Style == "" {
		missing = append(missing, "style")
	}
	if r.TotalPages == nil || *r.TotalPages <= 0 {
		missing = append(missing, "total_pages")
	}
	if r.Audience == nil || *r.Audience == "" {
		missing = append(missing, "audience")
	}
	return missing
}
