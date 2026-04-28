package tools

import (
	"context"
	"fmt"
	"os/exec"
)

// ExecuteCommand runs a shell command in the given working directory.
func ExecuteCommand(ctx context.Context, command string, workdir string) (string, string, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	if workdir != "" {
		cmd.Dir = workdir
	}
	out, err := cmd.CombinedOutput()
	stdout := string(out)
	stderr := ""
	if err != nil {
		if ctx.Err() != nil {
			return "", "", fmt.Errorf("command cancelled: %w", ctx.Err())
		}
		stderr = stdout
		return "", stderr, fmt.Errorf("command failed: %w", err)
	}
	return stdout, stderr, nil
}
