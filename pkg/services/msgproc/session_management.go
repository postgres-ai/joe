/*
2019 © Postgres.ai
*/

package msgproc

import (
	"context"
	"fmt"
	"strings"

	"github.com/pkg/errors"

	"gitlab.com/postgres-ai/database-lab/pkg/log"
	dblabmodels "gitlab.com/postgres-ai/database-lab/pkg/models"

	"gitlab.com/postgres-ai/joe/pkg/models"
	"gitlab.com/postgres-ai/joe/pkg/services/usermanager"
	"gitlab.com/postgres-ai/joe/pkg/util"
)

// CheckIdleSessions checks user idleness sessions and notifies about their finishing.
func (s *ProcessingService) CheckIdleSessions(ctx context.Context) {
	// List of channelIDs with a users to notify.
	channelsToNotify := make(map[string][]string)

	// List of sessionIDs.
	directToNotify := make([]string, 0)

	// TODO(akartasov): Fix data races.
	for _, user := range s.UserManager.Users() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if user == nil || user.Session.Clone == nil {
			continue
		}

		minutesAgoSinceLastAction := util.MinutesAgo(user.Session.LastActionTs)

		if minutesAgoSinceLastAction < user.Session.Clone.Metadata.MaxIdleMinutes {
			continue
		}

		if s.isActiveSession(ctx, user.Session.Clone.ID) {
			continue
		}

		log.Dbg("Session idle: %v %v", user, user.Session)

		if user.Session.Direct {
			directToNotify = append(directToNotify, getSessionID(user))
			continue
		}

		s.stopSession(user)

		channelsToNotify[user.Session.ChannelID] = append(channelsToNotify[user.Session.ChannelID], user.UserInfo.ID)
	}

	s.notifyDirectly(directToNotify)
	s.notifyChannels(channelsToNotify)
}

// notifyChannels publishes messages in every channel with a list of users.
func (s *ProcessingService) notifyChannels(channels map[string][]string) {
	for channelID, chatUserIDs := range channels {
		if len(chatUserIDs) == 0 {
			continue
		}

		formattedUserList := make([]string, 0, len(chatUserIDs))
		for _, chatUserID := range chatUserIDs {
			formattedUserList = append(formattedUserList, fmt.Sprintf("<@%s>", chatUserID))
		}

		msgText := "Stopped idle sessions for: " + strings.Join(formattedUserList, ", ")

		msg := models.NewMessage(models.IncomingMessage{ChannelID: channelID})
		msg.SetText(msgText)

		if err := s.messenger.Publish(msg); err != nil {
			log.Err("Bot: Cannot publish a message", err)
		}
	}
}

// notifyDirectly publishes a direct message about idle sessions.
func (s *ProcessingService) notifyDirectly(sessionList []string) {
	for _, sessionID := range sessionList {
		msg := models.NewMessage(models.IncomingMessage{})
		msg.SessionID = sessionID
		msg.SetStatus(models.StatusOK)
		msg.SetText("Stopped idle session")

		if err := s.messenger.Publish(msg); err != nil {
			log.Err("Bot: Cannot publish a direct message", err)
		}
	}
}

func getSessionID(u *usermanager.User) string {
	if u == nil || u.Session.Clone == nil || u.Session.Clone.ID == "" {
		return ""
	}

	sessionID := u.Session.Clone.ID

	// Use session ID from platform if it's defined.
	if u.Session.PlatformSessionID != "" {
		sessionID = u.Session.PlatformSessionID
	}

	return sessionID
}

// isActiveSession checks if current user session is active.
func (s *ProcessingService) isActiveSession(ctx context.Context, cloneID string) bool {
	clone, err := s.DBLab.GetClone(ctx, cloneID)
	if err != nil {
		return false
	}

	if clone.Status.Code != dblabmodels.StatusOK {
		return false
	}

	return true
}

func (s *ProcessingService) stopSession(user *usermanager.User) {
	user.Session.Clone = nil
	user.Session.ConnParams = models.Clone{}
	user.Session.PlatformSessionID = ""

	if user.Session.CloneConnection != nil {
		user.Session.CloneConnection.Close()
	}
}

// destroySession destroys a DatabaseLab session.
func (s *ProcessingService) destroySession(u *usermanager.User) error {
	log.Dbg("Stopping session...")

	if err := s.DBLab.DestroyClone(context.TODO(), u.Session.Clone.ID); err != nil {
		return errors.Wrap(err, "failed to destroy clone")
	}

	s.stopSession(u)

	return nil
}
