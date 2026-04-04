module educationagent/ppt_agent_service_go

go 1.22

require (
	github.com/chromedp/chromedp v0.9.5
	github.com/go-chi/chi/v5 v5.1.0
	github.com/golang-jwt/jwt/v5 v5.2.1
	github.com/google/uuid v1.6.0
	github.com/pmezard/go-difflib v1.0.0
	github.com/redis/go-redis/v9 v9.6.1
	github.com/openai/openai-go/v3 v3.26.0
	toolcalling v0.0.0
)

replace toolcalling => ../tool_calling_go
