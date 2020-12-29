package foreword

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestForeword(t *testing.T) {
	const (
		idleDuration = 20 * time.Minute
		sessionID    = "joe-bu8pgsrri60udnrile70"
		dbVersion    = "12.5 (Debian 12.5-1)"
		version      = "v0.7.2-20201225-0939"
		edition      = "CE"
		dataStateAt  = "2020-12-29 11:51:13 UTC"
		dbName       = "test"
		DSADiff      = "12h 34m"
		dbSize       = "730 GB"

		expectedForeword = "Say `help` to see the full list of commands.\n" +
			`Made with :hearts: by Postgres.ai. Bug reports, ideas, and merge requests are welcome: https://gitlab.com/postgres-ai/joe 
` + "```" + `
Session started: joe-bu8pgsrri60udnrile70
Idle session timeout: 20 minutes
Postgres version: 12.5 (Debian 12.5-1)
Joe version: v0.7.2-20201225-0939 (CE)
Database: test
Database size: 730 GB
Database state at: 2020-12-29 11:51:13 UTC (12h 34m ago)
` + "```"
	)

	forewordData := &Content{
		Duration:   idleDuration,
		SessionID:  sessionID,
		AppVersion: version,
		Edition:    edition,
		DBName:     dbName,
		DSA:        dataStateAt,
		DSADiff:    DSADiff,
		DBSize:     dbSize,
		DBVersion:  dbVersion,
	}

	assert.Equal(t, expectedForeword, forewordData.GetForeword())
}
