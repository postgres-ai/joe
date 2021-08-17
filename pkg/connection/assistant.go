/*
2019 © Postgres.ai
*/

package connection

import (
	"context"

	"gitlab.com/postgres-ai/joe/pkg/models"
	"gitlab.com/postgres-ai/joe/pkg/services/dblab"
	"gitlab.com/postgres-ai/joe/pkg/services/usermanager"
)

// Assistant defines the interface of a Query Optimization assistant.
type Assistant interface {
	// Init defines the method to initialize the assistant.
	Init() error

	// Register defines the method to register the assistant.
	Register(ctx context.Context, project string) error

	// Deregister defines the method to deregister the assistant.
	Deregister(ctx context.Context) error

	// RestoreSessions checks sessions after restart and establishes DB connection
	RestoreSessions(context.Context) error

	// CheckIdleSessions defines the method for checking user idle sessions and notification about them.
	CheckIdleSessions(context.Context)

	// AddChannel adds a new Database Lab instance to communication via the assistant.
	AddChannel(channelID, project string, dbLabInstance *dblab.Instance)

	// DumpSessions iterates over channels and collects user's sessions to storage
	DumpSessions()
}

// MessageProcessor defines the interface of a message processor.
type MessageProcessor interface {
	// ProcessMessageEvent defines the method for processing of incoming messages.
	ProcessMessageEvent(context.Context, models.IncomingMessage)

	// ProcessAppMentionEvent defines the method for replying to an application mention event.
	ProcessAppMentionEvent(incomingMessage models.IncomingMessage)

	// RestoreSessions checks sessions after restart and establishes DB connection
	RestoreSessions(ctx context.Context) error

	// Users returns user's session data
	Users() usermanager.UserList

	// CheckIdleSessions defines the method of check idleness sessions.
	CheckIdleSessions(ctx context.Context)
}
