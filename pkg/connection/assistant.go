/*
2019 © Postgres.ai
*/

package connection

import (
	"context"

	"gitlab.com/postgres-ai/joe/pkg/models"
	"gitlab.com/postgres-ai/joe/pkg/services/dblab"
)

// Assistant defines the interface of a Query Optimization assistant.
type Assistant interface {
	// Init defines the method to initialize the assistant.
	Init() error

	// CheckIdleSessions defines the method for checking user idle sessions and notification about them.
	CheckIdleSessions(context.Context)

	// AddDBLabInstanceForChannel adds a new Database Lab instance to communication via the assistant.
	AddDBLabInstanceForChannel(channelID string, dbLabInstance *dblab.Instance) error
}

// MessageProcessor defines the interface of a message processor.
type MessageProcessor interface {
	// ProcessMessageEvent defines the method for processing of incoming messages.
	ProcessMessageEvent(context.Context, models.IncomingMessage)

	// ProcessAppMentionEvent defines the method for replying to an application mention event.
	ProcessAppMentionEvent(incomingMessage models.IncomingMessage)

	// CheckIdleSessions defines the method of check idleness sessions.
	CheckIdleSessions(ctx context.Context)
}
