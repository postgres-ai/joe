/*
2026 © Postgres.ai
*/

package pgtransmission

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExecuteCommandContextCancel locks the H1 kill path: a hung child
// process must be killed when the context is done, instead of wedging the
// caller (and, transitively, the v2 session lock) forever.
func TestExecuteCommandContextCancel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()

	_, err := executeCommandContext(ctx, "sleep 30")

	assert.Error(t, err)
	assert.Less(t, time.Since(start), 10*time.Second,
		"the command must be killed by ctx, not run to completion")
}

// TestExecuteCommandContextCancelKillsGrandchild locks the H1 residual:
// killing only the direct bash child orphans any forked descendant, which
// keeps running (and keeps its server-side backend) after the caller
// returns. Bash exec-optimizes a lone simple command (even env-prefixed)
// into the same PID — that is what the bare `sleep 30` test above covers —
// but a compound command forks, and psql itself can fork children (e.g. a
// `\!` shell escape); the whole process group must die with the child.
func TestExecuteCommandContextCancelKillsGrandchild(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "grandchild.pid")

	// The trailing `:` makes the command compound, so bash forks instead of
	// exec-replacing itself; the inner sh writes its pid and exec-replaces
	// itself with sleep (same pid), standing in for a forked descendant.
	cmdStr := fmt.Sprintf("PGTEST=1 sh -c 'echo $$ > %s && exec sleep 30'; :", pidFile)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)

	go func() {
		_, err := executeCommandContext(ctx, cmdStr)
		done <- err
	}()

	var pid int

	require.Eventually(t, func() bool {
		raw, err := os.ReadFile(pidFile)
		if err != nil {
			return false
		}

		parsed, err := strconv.Atoi(strings.TrimSpace(string(raw)))
		if err != nil || parsed <= 0 {
			return false
		}

		pid = parsed

		return true
	}, 5*time.Second, 10*time.Millisecond, "the grandchild must report its pid")

	cancel()

	select {
	case err := <-done:
		assert.Error(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("executeCommandContext must return once ctx is cancelled")
	}

	assert.Eventually(t, func() bool {
		return syscall.Kill(pid, 0) == syscall.ESRCH
	}, 5*time.Second, 10*time.Millisecond,
		"the grandchild must die with the process group, not be orphaned")
}

func TestExecuteCommandContextSuccess(t *testing.T) {
	out, err := executeCommandContext(context.Background(), "echo -n hello")

	assert.NoError(t, err)
	assert.Equal(t, "hello", string(out))
}
