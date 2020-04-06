/*
2019 © Postgres.ai
*/

package features

import (
	"gitlab.com/postgres-ai/joe/features/definition"
)

// Pack defines enterprise feature helpers.
type Pack struct {
	cmdBuilder  CommandFactoryMethod
	entertainer definition.Entertainer
}

// NewPack creates a new features pack.
func NewPack() *Pack {
	return buildPack(GetBuilder(), GetEntertainer())
}

// buildPack builds a new features pack.
func buildPack(cmdBuilder CommandFactoryMethod, entertainer definition.Entertainer) *Pack {
	return &Pack{
		cmdBuilder:  cmdBuilder,
		entertainer: entertainer,
	}
}

// CmdBuilder provides a command factory method.
func (p *Pack) CmdBuilder() CommandFactoryMethod {
	return p.cmdBuilder
}

// Entertainer provides an entertainer service.
func (p *Pack) Entertainer() definition.Entertainer {
	return p.entertainer
}
