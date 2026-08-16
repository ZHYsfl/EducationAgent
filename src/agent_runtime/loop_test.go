package agent_runtime

import (
	"testing"
)

func TestValidateRequiredArgs(t *testing.T) {
	tests := []struct {
		name       string
		parameters map[string]any
		args       map[string]any
		want       []string
	}{
		{
			name:       "no parameters",
			parameters: nil,
			args:       map[string]any{},
			want:       nil,
		},
		{
			name: "all required present",
			parameters: map[string]any{
				"required": []any{"city", "date"},
			},
			args: map[string]any{
				"city": "Beijing",
				"date": "2024-01-01",
			},
			want: nil,
		},
		{
			name: "missing one required",
			parameters: map[string]any{
				"required": []any{"city", "date"},
			},
			args: map[string]any{
				"city": "Beijing",
			},
			want: []string{"date"},
		},
		{
			name: "missing multiple required",
			parameters: map[string]any{
				"required": []any{"city", "date", "unit"},
			},
			args: map[string]any{},
			want: []string{"city", "date", "unit"},
		},
		{
			name: "required field not a list",
			parameters: map[string]any{
				"required": "city",
			},
			args: map[string]any{},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validateRequiredArgs(tt.parameters, tt.args)
			if len(got) != len(tt.want) {
				t.Fatalf("validateRequiredArgs() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("validateRequiredArgs()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}
