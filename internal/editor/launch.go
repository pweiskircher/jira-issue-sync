package editor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func Launch(ctx context.Context, editor string, absolutePath string) error {
	parts := strings.Fields(editor)
	if len(parts) == 0 {
		return fmt.Errorf("invalid editor command")
	}

	args := append(parts[1:], absolutePath)
	command := exec.CommandContext(ctx, parts[0], args...)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("failed to launch editor: %w", err)
	}

	return nil
}
