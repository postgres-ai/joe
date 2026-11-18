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

	"gitlab.com/postgres-ai/joe/pkg/dblabapi"
)

func TestCloneGone(t *testing.T) {
	cloneWithStatus := func(code dblabapi.StatusCode) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(dblabapi.Clone{ID: "c", Status: dblabapi.Status{Code: code}})
		}
	}

	apiError := func(status int, code dblabapi.ErrorCode) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(dblabapi.Error{Code: code, Message: string(code)})
		}
	}

	tests := []struct {
		name    string
		handler http.HandlerFunc
		gone    bool
	}{
		{name: "deleted", handler: apiError(http.StatusNotFound, dblabapi.ErrCodeNotFound), gone: true},
		{name: "deleting", handler: cloneWithStatus(dblabapi.StatusDeleting), gone: true},
		{name: "fatal", handler: cloneWithStatus(dblabapi.StatusFatal), gone: true},
		{name: "ok", handler: cloneWithStatus(dblabapi.StatusOK), gone: false},
		{name: "resetting", handler: cloneWithStatus(dblabapi.StatusResetting), gone: false},
		{name: "api error", handler: apiError(http.StatusInternalServerError, dblabapi.ErrCodeInternal), gone: false},
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
		srv := httptest.NewServer(cloneWithStatus(dblabapi.StatusOK))
		srv.Close()

		client, err := dblabapi.NewClient(dblabapi.Options{Host: srv.URL})
		require.NoError(t, err)

		s := &ProcessingService{DBLab: client}
		require.False(t, s.cloneGone(context.Background(), "c"))
	})
}
