/*
2019 © Postgres.ai
*/

// Package dblab provides Database Lab instances.
package dblab

import (
	"gitlab.com/postgres-ai/database-lab/pkg/client/dblabapi"

	"gitlab.com/postgres-ai/joe/pkg/config"
)

// Instance contains a Database Lab client and its configuration.
type Instance struct {
	client *dblabapi.Client
	cfg    config.DBLabInstance
}

// NewDBLabInstance creates a new Database Lab Instance.
func NewDBLabInstance(client *dblabapi.Client, cfg config.DBLabInstance) *Instance {
	return &Instance{client: client, cfg: cfg}
}

// Client returns a Database Lab client of the instance.
func (d Instance) Client() *dblabapi.Client {
	return d.client
}

// Config returns a Database Lab config of the instance.
func (d Instance) Config() config.DBLabInstance {
	return d.cfg
}
