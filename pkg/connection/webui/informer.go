/*
2019 © Postgres.ai
*/

package webui

import (
	"gitlab.com/postgres-ai/joe/pkg/models"
)

// UserInformer provides a service for getting user info.
type UserInformer struct {
}

// NewUserInformer creates a new UserInformer service.
func NewUserInformer() UserInformer {
	return UserInformer{}
}

// GetUserInfo returns user info by ID.
func (m UserInformer) GetUserInfo(userID string) (models.UserInfo, error) {
	user := models.UserInfo{
		ID:       userID,
		Name:     userID,
		RealName: userID,
	}

	return user, nil
}
