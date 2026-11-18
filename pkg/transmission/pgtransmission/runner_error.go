/*
2026 © Postgres.ai
*/

package pgtransmission

import (
	"errors"
	"fmt"
	"os/exec"
)

// RunnerError describes a failed psql run.
type RunnerError struct {
	Msg        string
	ExitStatus int
	Stderr     string
}

// NewRunnerError wraps the result of a failed command run.
func NewRunnerError(command string, stderr string, err error) error {
	var runnerErr RunnerError
	if errors.As(err, &runnerErr) {
		return runnerErr
	}

	exitStatus := 0

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		exitStatus = exitErr.ExitCode()
	}

	return RunnerError{
		Msg:        fmt.Sprintf(`RunnerError(cmd="%s", inerr="%v", stderr="%s" exit="%d")`, command, err, stderr, exitStatus),
		ExitStatus: exitStatus,
		Stderr:     stderr,
	}
}

// Error returns the runner error message.
func (e RunnerError) Error() string {
	return e.Msg
}
