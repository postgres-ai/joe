/*
2019 © Postgres.ai
*/

package connection

import (
	"context"
)

// Assistant defines the interface of a Query Optimization assistant.
type Assistant interface {
	// Init defines the method to initialize the assistant.
	Init() error

	// CheckIdleSessions defines the method for checking user idle sessions and notification about them.
	CheckIdleSessions(context.Context)
}
