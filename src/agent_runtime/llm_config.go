package agent_runtime

import "time"

type LLMConfig struct {
	APIKey string // api key
	BaseURL string // the url of the model service
	Model string // the inference model name
	Temperature *float64 // the temperature of the model
	MaxTokens *int // the maximum number of tokens to generate
	TopP *float64 // the top_p parameter for nucleus sampling
	TopK *int // the top_k parameter for top-k sampling
	Timeout *time.Duration // the timeout for the request
	PresencePenalty *float64 // the presence_penalty parameter for the model
	FrequencyPenalty *float64 // the frequency_penalty parameter for the model
	ExtraBody map[string]any // extra body parameters for the request
}