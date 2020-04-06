/*
2019 © Postgres.ai
*/

// Package entertainer provides Enterprise entertainer service.
package entertainer

import (
	"gitlab.com/postgres-ai/joe/features/definition"
)

// Constants provide features description.
const (
	edition               = "Community Edition"
	enterpriseHelpMessage = "\n*Enterprise edition commands*:\n" +
		"• `activity` — show currently running sessions in Postgres (states: `active`, `idle in transaction`, `disabled`). Not supported in CE version\n" + //nolint:lll
		"• `terminate [pid]` — terminate Postgres backend that has the specified PID. Not supported in CE version\n"
)

// Entertainer implements entertainer interface for the Community edition.
type Entertainer struct {
}

var _ definition.Entertainer = (*Entertainer)(nil)

// New creates a new Entertainer for the Community edition.
func New() *Entertainer {
	return &Entertainer{}
}

// GetEnterpriseHelpMessage provides description of enterprise features.
func (e Entertainer) GetEnterpriseHelpMessage() string {
	return enterpriseHelpMessage
}

// GetEdition provides the assistant edition.
func (e Entertainer) GetEdition() string {
	return edition
}
