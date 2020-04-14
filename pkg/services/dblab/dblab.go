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
	cfg    config.DBLabParams
}

// NewDBLabInstance creates a new Database Lab Instance.
func NewDBLabInstance(client *dblabapi.Client) *Instance {
	return &Instance{client: client}
}

// Client returns a Database Lab client of the instance.
func (d Instance) Client() *dblabapi.Client {
	return d.client
}

// SetCfg sets database parameters of created clones.
func (d *Instance) SetCfg(cfg config.DBLabParams) {
	d.cfg = cfg
}

// Config returns database parameters of created clones.
func (d Instance) Config() config.DBLabParams {
	return d.cfg
}
