/*
2019 © Postgres.ai
*/

// Package definition provides basic Enterprise feature definitions.
package definition

// CmdBuilder provides a builder for Enterprise commands.
type CmdBuilder interface {
	BuildActivityCmd() Executor
	BuildTerminateCmd() Executor
}

// Executor describes a command interface.
type Executor interface {
	Execute() error
}
