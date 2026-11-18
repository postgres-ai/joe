/*
2019 © Postgres.ai
*/

package msgproc

import (
	"context"
	"fmt"
	"strings"

	"github.com/pkg/errors"

	"gitlab.com/postgres-ai/joe/pkg/dblabapi"
	"gitlab.com/postgres-ai/joe/pkg/log"

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
		if ctx.Err() != nil {
			return
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

		log.Dbg("Session idle: ", user, user.Session)

		if user.Session.Direct {
			directToNotify = append(directToNotify, getSessionID(user))
		} else {
			channelsToNotify[user.Session.ChannelID] = append(channelsToNotify[user.Session.ChannelID], user.UserInfo.ID)
		}

		s.stopSession(ctx, user)
	}

	s.notifyDirectly(directToNotify, models.StatusOK, "Stopped idle session")
	s.notifyChannels(channelsToNotify, func(chatUserIDs []string) string {
		formattedUserList := make([]string, 0, len(chatUserIDs))
		for _, chatUserID := range chatUserIDs {
			formattedUserList = append(formattedUserList, fmt.Sprintf("<@%s>", chatUserID))
		}

		return "Stopped idle sessions for: " + strings.Join(formattedUserList, ", ")
	})
}

// RestoreSessions checks sessions after restart and establishes DB connection.
func (s *ProcessingService) RestoreSessions(ctx context.Context) error {
	if len(s.UserManager.Users()) == 0 {
		return nil
	}

	// List of channelIDs with a users to notify.
	channelsToNotify := make(map[string][]string)

	// List of sessionIDs.
	directToNotify := make([]string, 0)

	for _, user := range s.UserManager.Users() {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if user == nil || user.Session.Clone == nil {
			continue
		}

		clone, err := s.DBLab.GetClone(ctx, user.Session.Clone.ID)
		if err != nil {
			log.Err("failed to get DBLab clone: ", err)
			s.stopSession(ctx, user)

			continue
		}

		if clone.Status.Code != dblabapi.StatusOK {
			log.Msg("DBLab is not active, stop user session. CloneID: ", user.Session.Clone.ID)
			s.stopSession(ctx, user)

			continue
		}

		if clone.DB.Port != user.Session.ConnParams.Port ||
			// we can't check hostname this way, looks like clone.DB.Host is depends on DLE config and could be localhost
			/*clone.DB.Host != user.Session.ConnParams.Host ||*/
			clone.DB.Username != user.Session.ConnParams.Username ||
			clone.DB.DBName != user.Session.ConnParams.Name {
			log.Msg("Session connection params has been changed in config. Stopping user session. CloneID: ", user.Session.Clone.ID)
			s.stopSession(ctx, user)

			continue
		}

		pool, userConn, err := initConn(ctx, user.Session.ConnParams)
		if err != nil {
			log.Err("failed to init database connection, stop session: ", err)
			s.stopSession(ctx, user)

			continue
		}

		user.Session.Clone = clone
		user.Session.Pool = pool
		user.Session.CloneConnection = userConn

		if user.Session.Direct {
			directToNotify = append(directToNotify, getSessionID(user))
		} else {
			channelsToNotify[user.Session.ChannelID] = append(channelsToNotify[user.Session.ChannelID], user.UserInfo.ID)
		}
	}

	s.notifyDirectly(directToNotify, models.StatusOK, fmt.Sprintf("Joe bot was restarted (version: %s).", s.config.App.Version))
	s.notifyChannels(channelsToNotify, func(chatUserIDs []string) string {
		formattedUserList := make([]string, 0, len(chatUserIDs))
		for _, chatUserID := range chatUserIDs {
			formattedUserList = append(formattedUserList, fmt.Sprintf("<@%s>", chatUserID))
		}

		return fmt.Sprintf("Joe bot was restarted (version: %s). Active sessions for: ", s.config.App.Version) +
			strings.Join(formattedUserList, ", ")
	})

	return nil
}

// notifyChannelsRestartSession publishes messages in every channel with a list of users.
func (s *ProcessingService) notifyChannels(channels map[string][]string, msgFormatter func(chatUserIDs []string) string) {
	for channelID, chatUserIDs := range channels {
		if len(chatUserIDs) == 0 {
			continue
		}

		msg := models.NewMessage(models.IncomingMessage{ChannelID: channelID})
		msg.SetText(msgFormatter(chatUserIDs))

		if err := s.messenger.Publish(msg); err != nil {
			log.Err("Bot: Cannot publish a message", err)
		}
	}
}

// notifyDirectly publishes a direct message about idle sessions.
func (s *ProcessingService) notifyDirectly(sessionList []string, status models.MessageStatus, message string) {
	for _, sessionID := range sessionList {
		msg := models.NewMessage(models.IncomingMessage{})
		msg.SessionID = sessionID
		msg.SetStatus(status)
		msg.SetText(message)

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

	if clone.Status.Code != dblabapi.StatusOK {
		return false
	}

	return true
}

// cloneGone reports whether the session clone was deleted or broke on the DBLab side.
// Unlike isActiveSession, a failed DBLab API call or a transient status (e.g. RESETTING
// during a concurrent reset) does not count, so a healthy session survives an API blip.
func (s *ProcessingService) cloneGone(ctx context.Context, cloneID string) bool {
	clone, err := s.DBLab.GetClone(ctx, cloneID)
	if err != nil {
		var apiErr dblabapi.Error

		return errors.As(err, &apiErr) && apiErr.Code == dblabapi.ErrCodeNotFound
	}

	return clone.Status.Code == dblabapi.StatusDeleting || clone.Status.Code == dblabapi.StatusFatal
}

// restartClosedSession replaces a session whose clone is gone with a new one.
func (s *ProcessingService) restartClosedSession(ctx context.Context, msg *models.Message, user *usermanager.User) {
	msg.AppendText("Session was closed by Database Lab.\n")

	if err := s.messenger.UpdateText(msg); err != nil {
		log.Err(fmt.Sprintf("failed to append message on session close: %+v", err))
	}

	s.stopSession(ctx, user)

	im := models.IncomingMessage{
		ChannelID: msg.ChannelID,
		CommandID: msg.CommandID,
	}

	if err := s.runSession(ctx, user, im); err != nil {
		log.Err(err)
	}
}

func (s *ProcessingService) stopSession(ctx context.Context, user *usermanager.User) {
	user.Session.Clone = nil
	user.Session.ConnParams = models.Clone{}
	user.Session.PlatformSessionID = ""

	if user.Session.CloneConnection != nil {
		if err := user.Session.CloneConnection.Close(ctx); err != nil {
			log.Err(err.Error())
		}
	}

	if user.Session.Pool != nil {
		// Close blocks until every acquired connection is released; a command still
		// running on this session must not block the caller.
		go user.Session.Pool.Close()
	}

	user.Session.CloneConnection = nil
	user.Session.Pool = nil
}

// destroySession destroys a DatabaseLab session.
func (s *ProcessingService) destroySession(ctx context.Context, u *usermanager.User) error {
	log.Dbg("Destroying session...")

	if u.Session.Clone != nil {
		if err := s.DBLab.DestroyClone(ctx, u.Session.Clone.ID); err != nil {
			return errors.Wrap(err, "failed to destroy clone")
		}
	}

	s.stopSession(ctx, u)

	return nil
}

// Users returns all tracked users with session data.
func (s *ProcessingService) Users() usermanager.UserList {
	return s.UserManager.Users()
}
