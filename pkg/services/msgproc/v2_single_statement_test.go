/*
2026 © Postgres.ai
*/

package msgproc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/postgres-ai/joe/pkg/services/usermanager"
)

// The v2 plan/explain/hypo runners execute over the simple protocol, where a
// semicolon-bearing command string runs as a multi-statement batch: a
// trailing `commit; <dml>` would escape the rolled-back transaction and
// persist writes on the clone (rev609/rev210 ship-blocker 3, M5 no-commit
// invariant). They must reject multi-statement SQL BEFORE touching the
// session (the users below carry no live connection: reaching the database
// layer would panic).

func TestRunV2PlanRejectsMultiStatement(t *testing.T) {
	s := &ProcessingService{}
	user := &usermanager.User{}

	result, err := s.runV2Plan(context.Background(), user,
		v2PlanEnvelopePrefix+"select 1; delete from users")

	assert.Nil(t, result)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "multi-statement")
}

func TestRunV2ExplainRejectsMultiStatement(t *testing.T) {
	s := &ProcessingService{}
	user := &usermanager.User{}

	result, err := s.runV2Explain(context.Background(), user,
		v2ExplainEnvelopePrefix+"select 1; commit; delete from users"+v2ExplainEnvelopeSuffix)

	assert.Nil(t, result)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "multi-statement")
}

func TestRunV2HypoRejectsMultiStatement(t *testing.T) {
	s := &ProcessingService{}
	user := &usermanager.User{}

	t.Run("multi-statement index definition", func(t *testing.T) {
		result, err := s.runV2Hypo(context.Background(), user,
			"create index on t (a); drop table t", "select 1")

		assert.Nil(t, result)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "multi-statement")
	})

	t.Run("multi-statement target query", func(t *testing.T) {
		result, err := s.runV2Hypo(context.Background(), user,
			"create index on t (a)", "select 1; commit; drop table t")

		assert.Nil(t, result)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "multi-statement")
	})
}
