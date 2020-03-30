/*
2019 © Postgres.ai
*/

// Package command provides assistant commands.
package command

import (
	"github.com/pkg/errors"

	"gitlab.com/postgres-ai/joe/features/definition"
)

// TerminateCmd defines the terminate command.
type TerminateCmd struct {
}

var _ definition.Executor = (*TerminateCmd)(nil)

// Execute runs the terminate command.
func (c *TerminateCmd) Execute() error {
	return errors.New("Enterprise feature. Not supported in CE version") // nolint:stylecheck
}
