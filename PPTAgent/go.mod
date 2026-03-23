module educationagent/pptagentgo

go 1.24

require (
	github.com/kenny-not-dead/gopptx v0.4.0
	github.com/nikolalohinski/gonja/v2 v2.4.2
	github.com/openai/openai-go/v3 v3.26.0
	gopkg.in/yaml.v3 v3.0.1
	toolcalling v0.0.0
)

replace toolcalling => ../tool_calling_go
