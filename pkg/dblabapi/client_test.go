/*
2026 © Postgres.ai
*/

package dblabapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testToken = "secret-token"

// fakeEngine serves the clone endpoints under /api. GET /api/clone/{id} answers
// with the queued statuses in order and repeats the last one; "" means 404.
type fakeEngine struct {
	t *testing.T

	mu        sync.Mutex
	statuses  []StatusCode
	gets      int
	requests  []string
	body      map[string]any
	postReply Clone
}

func (f *fakeEngine) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	assert.Equal(f.t, testToken, r.Header.Get(verificationHeader))

	f.requests = append(f.requests, r.Method+" "+r.URL.Path)

	if r.ContentLength > 0 {
		f.body = map[string]any{}
		assert.NoError(f.t, json.NewDecoder(r.Body).Decode(&f.body))
	}

	switch {
	case r.Method == http.MethodGet:
		status := f.statuses[min(f.gets, len(f.statuses)-1)]
		f.gets++

		if status == "" {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(Error{Code: ErrCodeNotFound, Message: "clone not found"})

			return
		}

		_ = json.NewEncoder(w).Encode(Clone{ID: "c1", Status: Status{Code: status}})

	case r.Method == http.MethodPost && r.URL.Path == "/api/clone":
		_ = json.NewEncoder(w).Encode(f.postReply)

	default:
		w.WriteHeader(http.StatusOK)
	}
}

func newTestClient(t *testing.T, engine http.Handler) *Client {
	t.Helper()

	srv := httptest.NewServer(engine)
	t.Cleanup(srv.Close)

	c, err := NewClient(Options{Host: srv.URL + "/api/", VerificationToken: testToken, RequestTimeout: time.Second})
	require.NoError(t, err)

	c.pollingInterval = time.Millisecond

	return c
}

func TestURLKeepsBasePath(t *testing.T) {
	c, err := NewClient(Options{Host: "https://dblab.example.com/api/"})
	require.NoError(t, err)

	assert.Equal(t, "https://dblab.example.com/api/clone/c1", c.URL("/clone/c1").String())
	assert.Equal(t, "dblab.example.com", c.URL("").Hostname())
}

func TestCreateCloneWaitsUntilReady(t *testing.T) {
	engine := &fakeEngine{
		t:         t,
		postReply: Clone{ID: "c1", Status: Status{Code: StatusCreating}},
		statuses:  []StatusCode{StatusCreating, StatusOK},
	}
	c := newTestClient(t, engine)

	clone, err := c.CreateClone(context.Background(), CloneCreateRequest{
		ID: "c1",
		DB: &DatabaseRequest{Username: "joe", Password: "pwd", Restricted: true, DBName: "db"},
	})
	require.NoError(t, err)

	assert.Equal(t, StatusOK, clone.Status.Code)
	assert.Equal(t, []string{"POST /api/clone", "GET /api/clone/c1", "GET /api/clone/c1"}, engine.requests)
	assert.Equal(t, map[string]any{"username": "joe", "password": "pwd", "restricted": true, "db_name": "db"},
		engine.body["db"])
}

func TestCreateCloneReadyImmediately(t *testing.T) {
	engine := &fakeEngine{t: t, postReply: Clone{ID: "c1", Status: Status{Code: StatusOK}}}
	c := newTestClient(t, engine)

	clone, err := c.CreateClone(context.Background(), CloneCreateRequest{ID: "c1"})
	require.NoError(t, err)

	assert.Equal(t, "c1", clone.ID)
	assert.Equal(t, []string{"POST /api/clone"}, engine.requests)
}

func TestCreateCloneFails(t *testing.T) {
	testCases := []struct {
		name      string
		postReply StatusCode
		statuses  []StatusCode
	}{
		{name: "empty status", postReply: ""},
		{name: "unexpected status", postReply: StatusResetting},
		{name: "fatal after creating", postReply: StatusCreating, statuses: []StatusCode{StatusFatal}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			engine := &fakeEngine{t: t, postReply: Clone{ID: "c1", Status: Status{Code: tc.postReply}}, statuses: tc.statuses}
			c := newTestClient(t, engine)

			_, err := c.CreateClone(context.Background(), CloneCreateRequest{ID: "c1"})
			assert.Error(t, err)
		})
	}
}

func TestResetCloneWaitsUntilReady(t *testing.T) {
	engine := &fakeEngine{t: t, statuses: []StatusCode{StatusResetting, StatusOK}}
	c := newTestClient(t, engine)

	require.NoError(t, c.ResetClone(context.Background(), "c1", ResetCloneRequest{}))
	assert.Equal(t, []string{"POST /api/clone/c1/reset", "GET /api/clone/c1", "GET /api/clone/c1"}, engine.requests)
}

func TestResetCloneFatal(t *testing.T) {
	c := newTestClient(t, &fakeEngine{t: t, statuses: []StatusCode{StatusFatal}})

	assert.Error(t, c.ResetClone(context.Background(), "c1", ResetCloneRequest{}))
}

func TestDestroyCloneWaitsUntilGone(t *testing.T) {
	engine := &fakeEngine{t: t, statuses: []StatusCode{StatusDeleting, ""}}
	c := newTestClient(t, engine)

	require.NoError(t, c.DestroyClone(context.Background(), "c1"))
	assert.Equal(t, []string{"DELETE /api/clone/c1", "GET /api/clone/c1", "GET /api/clone/c1"}, engine.requests)
}

func TestDestroyCloneStillPresent(t *testing.T) {
	c := newTestClient(t, &fakeEngine{t: t, statuses: []StatusCode{StatusOK}})

	assert.Error(t, c.DestroyClone(context.Background(), "c1"))
}

func TestWatchStopsAtRequestTimeout(t *testing.T) {
	c := newTestClient(t, &fakeEngine{t: t, statuses: []StatusCode{StatusResetting}})
	c.requestTimeout = 20 * time.Millisecond

	err := c.ResetClone(context.Background(), "c1", ResetCloneRequest{})
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestGetCloneNotFound(t *testing.T) {
	c := newTestClient(t, &fakeEngine{t: t, statuses: []StatusCode{""}})

	_, err := c.GetClone(context.Background(), "c1")
	require.Error(t, err)

	var apiErr Error
	require.True(t, errors.As(err, &apiErr))
	assert.Equal(t, ErrCodeNotFound, apiErr.Code)
}

func TestNonJSONErrorResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bad gateway", http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(Options{Host: srv.URL})
	require.NoError(t, err)

	_, err = c.GetClone(context.Background(), "c1")
	require.Error(t, err)

	var apiErr Error
	assert.False(t, errors.As(err, &apiErr))
	assert.Contains(t, err.Error(), "502")
	assert.Contains(t, err.Error(), "bad gateway")
}
