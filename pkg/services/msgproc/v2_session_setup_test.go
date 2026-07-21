/*
2026 © Postgres.ai
*/

package msgproc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/postgres-ai/database-lab/v3/pkg/client/dblabapi"
	dblabmodels "gitlab.com/postgres-ai/database-lab/v3/pkg/models"

	joemodels "gitlab.com/postgres-ai/joe/pkg/models"
	"gitlab.com/postgres-ai/joe/pkg/services/usermanager"
)

// TestEnsureV2SessionDestroysCloneOnConnFailure locks M1: when the clone is
// created but the database connection cannot be established, the clone MUST
// be destroyed — clone IDs are deterministic per session, so an orphaned
// clone would make every retry fail with "already exists" until DLE idle
// cleanup, while holding clone resources.
func TestEnsureV2SessionDestroysCloneOnConnFailure(t *testing.T) {
	var destroyed atomic.Int32

	dle := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/clone":
			// A "ready" clone whose database is unreachable (closed port).
			clone := dblabmodels.Clone{
				ID:     "v2-31",
				Status: dblabmodels.Status{Code: dblabmodels.StatusOK},
				DB: dblabmodels.Database{
					Host:     "127.0.0.1",
					Port:     "1",
					Username: "joe_test",
					DBName:   "test",
				},
			}

			w.Header().Set("Content-Type", "application/json")
			assert.NoError(t, json.NewEncoder(w).Encode(clone))
		case r.Method == http.MethodDelete && r.URL.Path == "/clone/v2-31":
			destroyed.Add(1)
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && r.URL.Path == "/clone/v2-31":
			// The destroy-watch poll: the clone is gone.
			w.Header().Set("Content-Type", "application/json")
			http.Error(w, `{"code":"NOT_FOUND","message":"the clone is gone"}`, http.StatusNotFound)
		default:
			http.Error(w, "unexpected call: "+r.Method+" "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer dle.Close()

	dbLabClient, err := dblabapi.NewClient(dblabapi.Options{Host: dle.URL, VerificationToken: "t"})
	require.NoError(t, err)

	s := &ProcessingService{DBLab: dbLabClient}

	user := &usermanager.User{
		UserInfo: joemodels.UserInfo{Name: "v2_session_31"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	err = s.ensureV2Session(ctx, user, "31")

	require.Error(t, err, "an unreachable clone database must fail the session setup")
	assert.EqualValues(t, 1, destroyed.Load(),
		"the just-created clone must be destroyed on connection failure (M1)")
	assert.Nil(t, user.Session.Clone)
}
