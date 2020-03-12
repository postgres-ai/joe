/*
2019 © Postgres.ai
*/

package bot

import (
	"context"
	"fmt"
	"net/http"
	"time"

	_ "github.com/lib/pq"
	"gitlab.com/postgres-ai/database-lab/pkg/log"

	"gitlab.com/postgres-ai/joe/pkg/config"
	"gitlab.com/postgres-ai/joe/pkg/connection"
	"gitlab.com/postgres-ai/joe/pkg/util"
)

// InactiveCloneCheckInterval defines an interval for check of idleness sessions.
const InactiveCloneCheckInterval = time.Minute

// App defines a application struct.
type App struct {
	Config config.Bot
}

// Creates a new application.
func NewApp(cfg config.Bot) *App {
	bot := App{
		Config: cfg,
	}
	return &bot
}

// RunServer starts a server for message processing.
func (b *App) RunServer(ctx context.Context, assistantSvc connection.Assistant) {
	if err := assistantSvc.Init(); err != nil {
		log.Fatal(err)
	}

	// Check idle sessions.
	_ = util.RunInterval(InactiveCloneCheckInterval, func() {
		log.Dbg("Check idle sessions")
		assistantSvc.CheckIdleSessions(ctx)
	})

	port := b.Config.Port
	log.Msg(fmt.Sprintf("Server start listening on localhost:%d", port))
	err := http.ListenAndServe(fmt.Sprintf(":%d", port), nil)
	log.Err("HTTP server error:", err)
}
