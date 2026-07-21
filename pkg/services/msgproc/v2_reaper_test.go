/*
2026 © Postgres.ai
*/

package msgproc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/postgres-ai/database-lab/v3/pkg/client/dblabapi"
	dblabmodels "gitlab.com/postgres-ai/database-lab/v3/pkg/models"

	"gitlab.com/postgres-ai/joe/features/definition"
	"gitlab.com/postgres-ai/joe/pkg/services/usermanager"
)

// newV2ReaperUser builds a v2 user whose session looks long idle.
func newV2ReaperUser(sessionID string, lastAction time.Time) *usermanager.User {
	return &usermanager.User{
		Session: usermanager.UserSession{
			PlatformSessionID: sessionID,
			Direct:            true,
			LastActionTs:      lastAction,
			Clone: &dblabmodels.Clone{
				ID:       v2CloneID(sessionID),
				Metadata: dblabmodels.CloneMetadata{MaxIdleMinutes: 60},
			},
		},
	}
}

// TestCheckIdleSessionsSkipsBusyV2Session locks H2: the v1 idle reaper must
// never touch a v2 session while a command holds its lock — the shared
// stopSession path nils Clone/Pool/CloneConnection under a running command
// and panics the process. (The service here deliberately carries NO DBLab
// client and NO messenger: the pre-fix reaper reaches both for v2 users.)
func TestCheckIdleSessionsSkipsBusyV2Session(t *testing.T) {
	user := newV2ReaperUser("31", time.Now().Add(-2*time.Hour))
	um := usermanager.NewUserManager(nil, definition.Quota{}, usermanager.UserList{
		v2UserPrefix + "31": user,
	})

	s := &ProcessingService{UserManager: um}

	unlock, ok := s.tryLockV2Session("31")
	require.True(t, ok, "the test must hold the session lock (command in flight)")
	defer unlock()

	s.CheckIdleSessions(context.Background())

	assert.NotNil(t, user.Session.Clone, "a busy v2 session must not be stopped")
}

// TestCheckIdleSessionsStopsIdleV2Session locks the flip side of H2: v2
// sessions must still be reaped when idle AND unlocked (no leak), under the
// session lock and WITHOUT the v1 chat notifications (the messenger is nil:
// notifying would panic).
func TestCheckIdleSessionsStopsIdleV2Session(t *testing.T) {
	// DLE stub: the clone is gone (404) — the same condition the v1 reaper
	// uses to clean Joe-side session state.
	dle := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"code":"NOT_FOUND"}`, http.StatusNotFound)
	}))
	defer dle.Close()

	dbLabClient, err := dblabapi.NewClient(dblabapi.Options{Host: dle.URL, VerificationToken: "t"})
	require.NoError(t, err)

	user := newV2ReaperUser("32", time.Now().Add(-2*time.Hour))
	um := usermanager.NewUserManager(nil, definition.Quota{}, usermanager.UserList{
		v2UserPrefix + "32": user,
	})

	s := &ProcessingService{UserManager: um, DBLab: dbLabClient}

	s.CheckIdleSessions(context.Background())

	assert.Nil(t, user.Session.Clone, "an idle v2 session with a dead clone must be cleaned up")

	// The reaper must have released the lock again.
	unlock, ok := s.tryLockV2Session("32")
	require.True(t, ok, "the session lock must be free after reaping")
	unlock()
}

// TestRestoreSessionsSkipsV2Users locks the restart half of H2: v2 sessions
// are rebuilt lazily by ensureV2Session; the v1 restore path must not touch
// them (nil DBLab client and nil messenger here would panic).
func TestRestoreSessionsSkipsV2Users(t *testing.T) {
	user := newV2ReaperUser("33", time.Now())
	um := usermanager.NewUserManager(nil, definition.Quota{}, usermanager.UserList{
		v2UserPrefix + "33": user,
	})

	s := &ProcessingService{UserManager: um}

	require.NoError(t, s.RestoreSessions(context.Background()))
	assert.NotNil(t, user.Session.Clone, "restore must leave the v2 session for lazy rebuild")
}
