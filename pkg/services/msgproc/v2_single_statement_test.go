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

func TestV2EnsureSingleStatement(t *testing.T) {
	valid := []struct {
		name string
		sql  string
	}{
		{"plain select", "select * from users where id = 1"},
		{"trailing semicolon", "select 1;"},
		{"trailing semicolon and whitespace", "select 1 ;  \n\t"},
		{"trailing semicolon and comment", "select 1; -- done"},
		{"trailing semicolon and block comment", "select 1; /* done */"},
		{"semicolon inside a string literal", "select * from t where name = 'a;b'"},
		{"doubled quote inside a string", "select 'it''s; fine'"},
		{"semicolon inside a quoted identifier", `select "col;umn" from t`},
		{"semicolon inside a dollar-quoted body", "select $$a; b$$"},
		{"semicolon inside a tagged dollar quote", "select $fn$begin; end$fn$"},
		{"dollar parameter placeholder", "select * from t where id = $1"},
		{"semicolon inside a line comment", "select 1 -- ; drop table t\n"},
		{"semicolon inside a block comment", "select 1 /* ; drop table t */"},
		{"semicolon inside a nested block comment", "select 1 /* a /* ; */ b */"},
	}

	for _, tc := range valid {
		t.Run("valid: "+tc.name, func(t *testing.T) {
			assert.NoError(t, v2EnsureSingleStatement(tc.sql))
		})
	}

	invalid := []struct {
		name string
		sql  string
	}{
		{"two statements", "select 1; select 2"},
		{"commit escape", "select 1; commit; delete from users"},
		{"statement after trailing comment", "select 1; -- x\n delete from users"},
		{"string after semicolon", "select 1; 'x'"},
		{"unterminated string hiding nothing", "select 1; delete from t where a = 'x"},
		{"backslash does not escape a quote", `select 'a\'; delete from users --'`},
	}

	for _, tc := range invalid {
		t.Run("invalid: "+tc.name, func(t *testing.T) {
			assert.Error(t, v2EnsureSingleStatement(tc.sql))
		})
	}
}

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
