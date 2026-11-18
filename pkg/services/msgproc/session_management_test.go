/*
2026 © Postgres.ai
*/

package msgproc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/postgres-ai/database-lab/v3/pkg/client/dblabapi"
	dblabmodels "gitlab.com/postgres-ai/database-lab/v3/pkg/models"
)

func TestCloneGone(t *testing.T) {
	cloneWithStatus := func(code dblabmodels.StatusCode) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(dblabmodels.Clone{ID: "c", Status: dblabmodels.Status{Code: code}})
		}
	}

	apiError := func(status int, code dblabmodels.ErrorCode) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(dblabmodels.Error{Code: code, Message: string(code)})
		}
	}

	tests := []struct {
		name    string
		handler http.HandlerFunc
		gone    bool
	}{
		{name: "deleted", handler: apiError(http.StatusNotFound, dblabmodels.ErrCodeNotFound), gone: true},
		{name: "deleting", handler: cloneWithStatus(dblabmodels.StatusDeleting), gone: true},
		{name: "fatal", handler: cloneWithStatus(dblabmodels.StatusFatal), gone: true},
		{name: "ok", handler: cloneWithStatus(dblabmodels.StatusOK), gone: false},
		{name: "resetting", handler: cloneWithStatus(dblabmodels.StatusResetting), gone: false},
		{name: "api error", handler: apiError(http.StatusInternalServerError, dblabmodels.ErrCodeInternal), gone: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(tt.handler)
			defer srv.Close()

			client, err := dblabapi.NewClient(dblabapi.Options{Host: srv.URL})
			require.NoError(t, err)

			s := &ProcessingService{DBLab: client}
			require.Equal(t, tt.gone, s.cloneGone(context.Background(), "c"))
		})
	}

	t.Run("dblab unreachable", func(t *testing.T) {
		srv := httptest.NewServer(cloneWithStatus(dblabmodels.StatusOK))
		srv.Close()

		client, err := dblabapi.NewClient(dblabapi.Options{Host: srv.URL})
		require.NoError(t, err)

		s := &ProcessingService{DBLab: client}
		require.False(t, s.cloneGone(context.Background(), "c"))
	})
}
