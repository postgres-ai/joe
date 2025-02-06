/*
2021 © Postgres.ai
*/

package platform

import (
	"context"

	"github.com/pkg/errors"

	"gitlab.com/postgres-ai/database-lab/v3/pkg/log"
)

// RegisterApplicationRequest represents a request of the application registration.
type RegisterApplicationRequest struct {
	OrgID        uint   `json:"org_id"`
	URL          string `json:"url"`
	Token        string `json:"token"`
	Project      string `json:"project"`
	SSHServerURL string `json:"ssh_server_url"`
	UseTunnel    bool   `json:"use_tunnel"`
	DryRun       bool   `json:"dry_run"`
}

// RegisterApplicationResponse represents a response of the application registration.
type RegisterApplicationResponse struct {
	APIResponse
	InstanceID uint64 `json:"id"`
}

// DeregisterApplicationRequest represents a request of the application deregistration.
type DeregisterApplicationRequest struct {
	InstanceID uint64 `json:"instance_id"`
}

// DeregisterApplicationResponse represents a response of the application deregistration.
type DeregisterApplicationResponse struct {
	APIResponse
	InstanceID uint64 `json:"result"`
}

// RegisterApplication register the application on the Platform.
func (p *Client) RegisterApplication(ctx context.Context, registerRequest RegisterApplicationRequest) (uint64, error) {
	log.Dbg("Platform API: register application: ", registerRequest.Project)

	respData := RegisterApplicationResponse{}

	if err := p.doPost(ctx, "/rpc/joe_instance_create", registerRequest, &respData); err != nil {
		return 0, errors.Wrap(err, "failed to do request")
	}

	if respData.Code != "" || respData.Message != "" {
		return 0, errors.Errorf("error: %v", respData)
	}

	log.Dbg("Platform API: application has been successfully registered", respData.InstanceID)

	return respData.InstanceID, nil
}

// DeregisterApplication deregister the application from the Platform.
func (p *Client) DeregisterApplication(ctx context.Context, deregisterRequest DeregisterApplicationRequest) error {
	log.Dbg("Platform API: deregister application: ", deregisterRequest.InstanceID)

	respData := DeregisterApplicationResponse{}

	if err := p.doPost(ctx, "/rpc/joe_instance_destroy", deregisterRequest, &respData); err != nil {
		return errors.Wrap(err, "failed to do request")
	}

	if respData.Code != "" || respData.Message != "" {
		return errors.Errorf("error: %v", respData)
	}

	log.Dbg("Platform API: application has been successfully deregistered. Instance ID: ", respData.InstanceID)

	return nil
}
