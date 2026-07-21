/*
2026 © Postgres.ai
*/

package pgtransmission

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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

func TestExecuteCommandContextSuccess(t *testing.T) {
	out, err := executeCommandContext(context.Background(), "echo -n hello")

	assert.NoError(t, err)
	assert.Equal(t, "hello", string(out))
}
