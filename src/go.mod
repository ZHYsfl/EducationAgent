module educationagent

go 1.25.0

require (
	agent_runtime v0.0.0-00010101000000-000000000000
	github.com/coder/websocket v1.8.15
	github.com/openai/openai-go/v3 v3.51.0
)

require (
	github.com/tidwall/gjson v1.19.0 // indirect
	github.com/tidwall/match v1.1.1 // indirect
	github.com/tidwall/pretty v1.2.1 // indirect
	github.com/tidwall/sjson v1.2.5 // indirect
)

replace agent_runtime => ./agent_runtime
