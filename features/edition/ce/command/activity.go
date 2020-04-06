/*
2019 © Postgres.ai
*/

// Package command provides assistant commands.
package command

import (
	"errors"

	"gitlab.com/postgres-ai/joe/features/definition"
)

// ActivityCmd defines the activity command.
type ActivityCmd struct {
}

var _ definition.Executor = (*ActivityCmd)(nil)

// Execute runs the activity command.
func (c *ActivityCmd) Execute() error {
	return errors.New("feature is locked. Not supported in CE version. Upgrade plan")
}
