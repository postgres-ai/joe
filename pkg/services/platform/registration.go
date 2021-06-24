/*
2021 © Postgres.ai
*/

package platform

import (
	"context"

	"github.com/pkg/errors"

	"gitlab.com/postgres-ai/database-lab/v2/pkg/log"
)

// RegisterApplicationRequest represents a request of the application registration.
type RegisterApplicationRequest struct {
	URL     string `json:"url"`
	Project string `json:"project"`
}

// RegisterApplicationResponse represents a response of the application registration.
type RegisterApplicationResponse struct {
	APIResponse
	InstanceID string `json:"instance_id"`
}

// DeregisterApplicationRequest represents a request of the application deregistration.
type DeregisterApplicationRequest struct {
	InstanceID string `json:"instance_id"`
}

// RegisterApplication register the application on the Platform.
func (p *Client) RegisterApplication(ctx context.Context, registerRequest RegisterApplicationRequest) (string, error) {
	log.Dbg("Platform API: register application")

	respData := RegisterApplicationResponse{}

	if err := p.doPost(ctx, "/rpc/joe_instance_create", registerRequest, &respData); err != nil {
		return "", errors.Wrap(err, "failed to do request")
	}

	if respData.Code != "" || respData.Message != "" {
		return "", errors.Errorf("error: %v", respData)
	}

	log.Dbg("Platform API: application has been successfully registered", respData.InstanceID)

	return respData.InstanceID, nil
}

// DeregisterApplication deregister the application from the Platform.
func (p *Client) DeregisterApplication(ctx context.Context, deregisterRequest DeregisterApplicationRequest) error {
	log.Dbg("Platform API: deregister application")

	respData := APIResponse{}

	if err := p.doPost(ctx, "/rpc/joe_instance_delete", deregisterRequest, &respData); err != nil {
		return errors.Wrap(err, "failed to do request")
	}

	if respData.Code != "" || respData.Message != "" {
		return errors.Errorf("error: %v", respData)
	}

	log.Dbg("Platform API: application has been successfully deregistered")

	return nil
}
