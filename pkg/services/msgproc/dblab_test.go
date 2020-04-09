package msgproc

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestForeword(t *testing.T) {
	const (
		idleDuration = 20 * time.Minute
		version      = "v1.0.0"
		edition      = "CE"
		dataStateAt  = "2020-04-06 11:30:00 UTC"
		dbName       = "testdb"

		expectedForeword = `• Say 'help' to see the full list of commands.
• Sessions are fully independent. Feel free to do anything.
• The session will be destroyed after 20 minutes of inactivity.
• EXPLAIN plans here are expected to be identical to production plans.
• The actual timing values may differ from production because actual caches in DB Lab are smaller. However, the number of bytes and pages/buffers in plans are identical to production.

Made with :hearts: by Postgres.ai. Bug reports, ideas, and merge requests are welcome: https://gitlab.com/postgres-ai/joe 

Joe version: v1.0.0 (CE).
Database: testdb. Snapshot data state at: 2020-04-06 11:30:00 UTC.`
	)

	foreword := getForeword(idleDuration, version, edition, dataStateAt, dbName)

	assert.Equal(t, expectedForeword, foreword)
}
