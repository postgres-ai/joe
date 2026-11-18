/*
2026 © Postgres.ai
*/

// Package dblabapi is a client for the clone endpoints of the DBLab Engine API.
package dblabapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/pkg/errors"

	"gitlab.com/postgres-ai/joe/pkg/log"
)

const (
	verificationHeader = "Verification-Token"

	defaultPollingInterval = time.Second
	defaultRequestTimeout  = time.Minute
)

// Options configures a Client.
type Options struct {
	Host              string
	VerificationToken string

	// RequestTimeout bounds how long CreateClone, ResetClone, and DestroyClone wait
	// for the clone to leave its transitional status when ctx has no deadline.
	RequestTimeout time.Duration
}

// Client is a DBLab Engine API client.
type Client struct {
	url               *url.URL
	verificationToken string
	client            *http.Client
	requestTimeout    time.Duration
	pollingInterval   time.Duration
}

// NewClient creates a DBLab Engine API client.
func NewClient(options Options) (*Client, error) {
	u, err := url.Parse(options.Host)
	if err != nil {
		return nil, err
	}

	u.Path = strings.TrimRight(u.Path, "/")

	if options.RequestTimeout == 0 {
		options.RequestTimeout = defaultRequestTimeout
	}

	// Like the database-lab v3 client this replaces, DBLab requests ignore
	// HTTP(S)_PROXY: deployments that proxy Slack egress need not exempt DBLab.
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil

	return &Client{
		url:               u,
		verificationToken: options.VerificationToken,
		client:            &http.Client{Transport: transport},
		requestTimeout:    options.RequestTimeout,
		pollingInterval:   defaultPollingInterval,
	}, nil
}

// URL builds the URL of an API endpoint.
func (c *Client) URL(endpoint string) *url.URL {
	u := *c.url
	u.Path = path.Join(c.url.Path, endpoint)

	return &u
}

// GetClone returns a clone.
func (c *Client) GetClone(ctx context.Context, cloneID string) (*Clone, error) {
	var clone Clone

	if err := c.request(ctx, http.MethodGet, "/clone/"+cloneID, nil, &clone); err != nil {
		return nil, err
	}

	return &clone, nil
}

// CreateClone creates a clone and waits until it is ready.
func (c *Client) CreateClone(ctx context.Context, cloneRequest CloneCreateRequest) (*Clone, error) {
	var clone Clone

	if err := c.request(ctx, http.MethodPost, "/clone", cloneRequest, &clone); err != nil {
		return nil, err
	}

	switch clone.Status.Code {
	case StatusOK:
		return &clone, nil

	case StatusCreating:

	case "":
		return nil, errors.New("empty clone status given")

	default:
		return nil, errors.Errorf("unexpected clone status given: %v", clone.Status)
	}

	created, err := c.watchCloneStatus(ctx, clone.ID, StatusCreating)
	if err != nil {
		return nil, errors.Wrap(err, "failed to watch the clone status")
	}

	if created.Status.Code != StatusOK {
		return nil, errors.Errorf("failed to create clone, unexpected status given. %v: %s",
			created.Status.Code, created.Status.Message)
	}

	return created, nil
}

// ResetClone resets a clone and waits until it is ready.
func (c *Client) ResetClone(ctx context.Context, cloneID string, params ResetCloneRequest) error {
	if err := c.request(ctx, http.MethodPost, "/clone/"+cloneID+"/reset", params, nil); err != nil {
		return err
	}

	clone, err := c.watchCloneStatus(ctx, cloneID, StatusResetting)
	if err != nil {
		return errors.Wrap(err, "failed to watch the clone status")
	}

	if clone.Status.Code != StatusOK {
		return errors.Errorf("unexpected clone status given: %v", clone.Status)
	}

	return nil
}

// DestroyClone deletes a clone and waits until it is gone.
func (c *Client) DestroyClone(ctx context.Context, cloneID string) error {
	if err := c.request(ctx, http.MethodDelete, "/clone/"+cloneID, nil, nil); err != nil {
		return err
	}

	clone, err := c.watchCloneStatus(ctx, cloneID, StatusDeleting)
	if err != nil {
		var apiErr Error
		if errors.As(err, &apiErr) && apiErr.Code == ErrCodeNotFound {
			return nil
		}

		return errors.Wrap(err, "failed to watch the clone status")
	}

	return errors.Errorf("unexpected clone status given: %v", clone.Status)
}

// watchCloneStatus polls the clone until its status differs from initialStatus.
// Without a deadline on ctx, polling stops after the client's request timeout.
func (c *Client) watchCloneStatus(ctx context.Context, cloneID string, initialStatus StatusCode) (*Clone, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc

		ctx, cancel = context.WithTimeout(ctx, c.requestTimeout)
		defer cancel()
	}

	pollingTimer := time.NewTimer(c.pollingInterval)
	defer pollingTimer.Stop()

	for {
		select {
		case <-pollingTimer.C:
			clone, err := c.GetClone(ctx, cloneID)
			if err != nil {
				return nil, errors.Wrap(err, "failed to get clone info")
			}

			if clone.Status.Code != initialStatus {
				return clone, nil
			}

			pollingTimer.Reset(c.pollingInterval)

		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// request sends a JSON request and decodes the response into out unless it is nil.
// An error response is returned as Error when its body decodes as one.
func (c *Client) request(ctx context.Context, method, endpoint string, in, out any) error {
	var body io.Reader

	if in != nil {
		buf := &bytes.Buffer{}
		if err := json.NewEncoder(buf).Encode(in); err != nil {
			return errors.Wrap(err, "failed to encode the request body")
		}

		body = buf
	}

	u := c.URL(endpoint)

	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return errors.Wrap(err, "failed to make a request")
	}

	req.Header.Set(verificationHeader, c.verificationToken)

	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return errors.Wrap(err, "failed to get response")
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= http.StatusBadRequest {
		// Request bodies and headers are left out of the log: they carry the
		// verification token and clone passwords.
		log.Dbg(fmt.Sprintf("DBLab API request failed: %s %s: %s", method, u.Path, resp.Status))

		return errors.Wrap(decodeError(resp), "failed to get response")
	}

	if out == nil {
		return nil
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return errors.Wrap(err, "failed to decode a response body")
	}

	return nil
}

// decodeError turns an error response into Error, or into a plain error when
// the body is not a DBLab error object.
func decodeError(resp *http.Response) error {
	const maxErrorBody = 64 << 10

	b, err := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
	if err != nil {
		return errors.Wrapf(err, "failed to read an error response (%s)", resp.Status)
	}

	var apiErr Error
	if err := json.Unmarshal(b, &apiErr); err != nil || apiErr.Code == "" {
		return errors.Errorf("unexpected response %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}

	return apiErr
}
