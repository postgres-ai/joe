/*
2019 © Postgres.ai
*/

package slackrtm

import (
	"github.com/pkg/errors"
	"github.com/slack-go/slack"

	"gitlab.com/postgres-ai/joe/pkg/models"
)

// UserInformer provides a service for getting user info.
type UserInformer struct {
	rtm *slack.RTM
}

// NewUserInformer creates a new UserInformer service.
func NewUserInformer(rtm *slack.RTM) *UserInformer {
	return &UserInformer{
		rtm: rtm,
	}
}

// GetUserInfo retrieves user info by ID.
func (m *UserInformer) GetUserInfo(userID string) (models.UserInfo, error) {
	slackUser, err := m.rtm.GetUserInfo(userID)
	if err != nil {
		return models.UserInfo{}, errors.Wrap(err, "failed to get user info")
	}

	user := models.UserInfo{
		ID:       slackUser.ID,
		Name:     slackUser.Name,
		RealName: slackUser.RealName,
	}

	return user, nil
}
