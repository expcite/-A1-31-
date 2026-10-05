package console

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"time"
)

// Callers resolve only the fixed scenarios catalogue. WaitDelay bounds waiting
// on inherited stdout/stderr pipes after exit or context cancellation; a zero
// process exit with incomplete output remains an evidence failure.
func runJobCommand(ctx context.Context, binary string, args []string, dir string, env []string, output io.Writer) (int, error) {
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir, cmd.Env = dir, env
	cmd.Stdout, cmd.Stderr = output, output
	cmd.WaitDelay = 2 * time.Second
	err := cmd.Run()
	code := -1
	if cmd.ProcessState != nil {
		code = cmd.ProcessState.ExitCode()
	}
	return code, errors.Join(err, ctx.Err())
}
