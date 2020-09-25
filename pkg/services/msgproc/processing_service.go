/*
2019 © Postgres.ai
*/

package msgproc

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"
	"unicode"

	"github.com/pkg/errors"

	"gitlab.com/postgres-ai/database-lab/pkg/client/dblabapi"
	"gitlab.com/postgres-ai/database-lab/pkg/log"
	"gitlab.com/postgres-ai/database-lab/pkg/util"

	"gitlab.com/postgres-ai/joe/features"
	"gitlab.com/postgres-ai/joe/features/definition"
	"gitlab.com/postgres-ai/joe/pkg/bot/command"
	"gitlab.com/postgres-ai/joe/pkg/config"
	"gitlab.com/postgres-ai/joe/pkg/connection"
	"gitlab.com/postgres-ai/joe/pkg/models"
	"gitlab.com/postgres-ai/joe/pkg/pgexplain"
	"gitlab.com/postgres-ai/joe/pkg/services/platform"
	"gitlab.com/postgres-ai/joe/pkg/services/usermanager"
	"gitlab.com/postgres-ai/joe/pkg/transmission/pgtransmission"
	"gitlab.com/postgres-ai/joe/pkg/util/text"
)

// Constants declare supported commands.
const (
	CommandExplain   = "explain"
	CommandExec      = "exec"
	CommandReset     = "reset"
	CommandHelp      = "help"
	CommandHypo      = "hypo"
	CommandActivity  = "activity"
	CommandTerminate = "terminate"
	CommandPlan      = "plan"

	CommandPsqlD   = `\d`
	CommandPsqlDP  = `\d+`
	CommandPsqlDT  = `\dt`
	CommandPsqlDTP = `\dt+`
	CommandPsqlDI  = `\di`
	CommandPsqlDIP = `\di+`
	CommandPsqlL   = `\l`
	CommandPsqlLP  = `\l+`
	CommandPsqlDV  = `\dv`
	CommandPsqlDVP = `\dv+`
	CommandPsqlDM  = `\dm`
	CommandPsqlDMP = `\dm+`
)

var supportedCommands = []string{
	CommandExplain,
	CommandPlan,
	CommandHypo,
	CommandExec,
	CommandReset,
	CommandActivity,
	CommandTerminate,
	CommandHelp,

	CommandPsqlD,
	CommandPsqlDP,
	CommandPsqlDT,
	CommandPsqlDTP,
	CommandPsqlDI,
	CommandPsqlDIP,
	CommandPsqlL,
	CommandPsqlLP,
	CommandPsqlDV,
	CommandPsqlDVP,
	CommandPsqlDM,
	CommandPsqlDMP,
}

var allowedPsqlCommands = []string{
	CommandPsqlD,
	CommandPsqlDP,
	CommandPsqlDT,
	CommandPsqlDTP,
	CommandPsqlDI,
	CommandPsqlDIP,
	CommandPsqlL,
	CommandPsqlLP,
	CommandPsqlDV,
	CommandPsqlDVP,
	CommandPsqlDM,
	CommandPsqlDMP,
}

type ProcessingService struct {
	featurePack      *features.Pack
	messageValidator connection.MessageValidator
	messenger        connection.Messenger
	DBLab            *dblabapi.Client
	UserManager      *usermanager.UserManager
	platformManager  *platform.Client
	config           ProcessingConfig

	// TODO (akartasov): Add specific services.
	//Auditor
	//Limiter
}

// ProcessingConfig declares a configuration of Processing Service.
type ProcessingConfig struct {
	App      config.App
	Platform config.Platform
	Explain  pgexplain.ExplainConfig
	DBLab    config.DBLabParams
	EntOpts  definition.EnterpriseOptions
	Project  string
}

// NewProcessingService creates a new processing service.
func NewProcessingService(messengerSvc connection.Messenger, msgValidator connection.MessageValidator, dblab *dblabapi.Client,
	userSvc *usermanager.UserManager, platform *platform.Client, cfg ProcessingConfig,
	featurePack *features.Pack) *ProcessingService {
	return &ProcessingService{
		featurePack:      featurePack,
		messageValidator: msgValidator,
		messenger:        messengerSvc,
		DBLab:            dblab,
		UserManager:      userSvc,
		platformManager:  platform,
		config:           cfg,
	}
}

// ProcessMessageEvent replies to a message.
func (s *ProcessingService) ProcessMessageEvent(ctx context.Context, incomingMessage models.IncomingMessage) {
	// Filter incoming message.
	if err := s.messageValidator.Validate(&incomingMessage); err != nil {
		log.Err(errors.Wrap(err, "incoming message is invalid"))
		return
	}

	// Get user or create a new one.
	user, err := s.UserManager.CreateUser(incomingMessage.UserID)
	if err != nil {
		log.Err(errors.Wrap(err, "failed to get user"))

		if err := s.messenger.Fail(models.NewMessage(incomingMessage), err.Error()); err != nil {
			log.Err(errors.Wrap(err, "failed to get user"))
			return
		}

		return
	}

	if err := s.prepareUserSession(user, incomingMessage); err != nil {
		log.Err(err)
		return
	}

	// Filter and prepare message.
	message := strings.TrimSpace(incomingMessage.Text)
	message = strings.Trim(message, "`")
	message = formatMessage(message)

	// Get command from snippet if exists. Snippets allow longer queries support.
	if incomingMessage.SnippetURL != "" {
		log.Dbg("Using attached file as message")

		snippet, err := s.messenger.DownloadArtifact(incomingMessage.SnippetURL)
		if err != nil {
			log.Err(err)

			if err := s.messenger.Fail(models.NewMessage(incomingMessage), err.Error()); err != nil {
				log.Err(errors.Wrap(err, "failed to download artifact"))
				return
			}

			return
		}

		message = string(snippet)
	}

	if len(message) == 0 {
		log.Dbg("Message filtered: Empty")
		return
	}

	receivedCommand, query := parseIncomingMessage(message)

	s.showBotHints(incomingMessage, receivedCommand, query)

	if !util.Contains(supportedCommands, receivedCommand) {
		log.Dbg("Message filtered: Not a command")
		return
	}

	if err := user.RequestQuota(); err != nil {
		log.Err("Quota: ", err)

		if err := s.messenger.Fail(models.NewMessage(incomingMessage), err.Error()); err != nil {
			log.Err(errors.Wrap(err, "failed to request quotas"))
			return
		}

		return
	}

	// We want to save message height space for more valuable info.
	queryPreview := strings.ReplaceAll(query, "\n", " ")
	queryPreview = strings.ReplaceAll(queryPreview, "\t", " ")
	queryPreview, _ = text.CutText(queryPreview, QueryPreviewSize, SeparatorEllipsis)

	if s.config.EntOpts.Audit.Enabled {
		audit, err := json.Marshal(models.Audit{
			ID:       user.UserInfo.ID,
			Name:     user.UserInfo.Name,
			RealName: user.UserInfo.RealName,
			Command:  receivedCommand,
			Query:    query,
		})

		if err != nil {
			if err := s.messenger.Fail(models.NewMessage(incomingMessage), err.Error()); err != nil {
				log.Err(errors.Wrap(err, "failed to marshal Audit struct"))
				return
			}

			return
		}

		log.Audit(string(audit))
	}

	msgText := fmt.Sprintf("```%s %s```\n", receivedCommand, queryPreview)

	// Show `help` command without initializing of a session.
	if receivedCommand == CommandHelp {
		msg := models.NewMessage(incomingMessage)

		msgText = s.appendHelp(msgText)
		msgText = appendSessionID(msgText, user)
		msg.SetText(msgText)

		if err := s.messenger.Publish(msg); err != nil {
			// TODO(anatoly): Retry.
			log.Err("Bot: Cannot publish a message", err)
		}

		return
	}

	if err := s.runSession(ctx, user, incomingMessage); err != nil {
		log.Err(err)
		return
	}

	msg := models.NewMessage(incomingMessage)

	msgText = appendSessionID(msgText, user)
	msg.SetText(msgText)

	if err := s.messenger.Publish(msg); err != nil {
		// TODO(anatoly): Retry.
		log.Err("Bot: Cannot publish a message", err)
		return
	}

	if err := msg.SetNotifyAt(s.config.App.MinNotifyDuration); err != nil {
		log.Err(err)
	}

	msg.SetUserID(user.UserInfo.ID)

	if err := s.messenger.UpdateStatus(msg, models.StatusRunning); err != nil {
		log.Err(err)
	}

	platformCmd := &platform.Command{
		SessionID: user.Session.PlatformSessionID,
		Command:   receivedCommand,
		Query:     query,
		Timestamp: incomingMessage.Timestamp,
	}

	switch {
	case receivedCommand == CommandExplain:
		err = command.Explain(s.messenger, platformCmd, msg, s.config.Explain, user.Session.CloneConnection)

	case receivedCommand == CommandPlan:
		planCmd := command.NewPlan(platformCmd, msg, user.Session.CloneConnection, s.messenger)
		err = planCmd.Execute(ctx)

	case receivedCommand == CommandExec:
		execCmd := command.NewExec(platformCmd, msg, user.Session.CloneConnection, s.messenger)
		err = execCmd.Execute()

	case receivedCommand == CommandReset:
		err = command.ResetSession(ctx, platformCmd, msg, s.DBLab, user.Session.Clone.ID, s.messenger, user.Session.CloneConnection)
		// TODO(akartasov): Find permanent solution,
		//  it's a temporary fix for https://gitlab.com/postgres-ai/joe/-/issues/132.
		if err != nil {
			log.Err(fmt.Sprintf("Failed to reset session: %v. Trying to reboot session.", err))

			// Try to reboot the session.
			if err := s.rebootSession(msg, user); err != nil {
				log.Err(err)
			}

			return
		}

	case receivedCommand == CommandHypo:
		hypoCmd := command.NewHypo(platformCmd, msg, user.Session.CloneConnection, s.messenger)
		err = hypoCmd.Execute()

	case receivedCommand == CommandActivity:
		activityCmd := command.NewActivityCmd(platformCmd, msg, user.Session.CloneConnection, s.messenger)
		err = activityCmd.Execute()

	case receivedCommand == CommandTerminate:
		terminateCmd := command.NewTerminateCmd(platformCmd, msg, user.Session.CloneConnection, s.messenger)
		err = terminateCmd.Execute()

	case util.Contains(allowedPsqlCommands, receivedCommand):
		runner := pgtransmission.NewPgTransmitter(user.Session.ConnParams, pgtransmission.LogsEnabledDefault)
		err = command.Transmit(platformCmd, msg, s.messenger, runner)
	}

	if err != nil {
		if _, ok := err.(*net.OpError); !ok {
			if err := s.messenger.Fail(msg, err.Error()); err != nil {
				log.Err(err)
			}

			return
		}

		if !s.isActiveSession(ctx, user.Session.Clone.ID) {
			msg.AppendText("Session was closed by Database Lab.\n")
			if err := s.messenger.UpdateText(msg); err != nil {
				log.Err(fmt.Sprintf("failed to append message on session close: %+v", err))
			}
			s.stopSession(user)

			im := models.IncomingMessage{
				ChannelID: msg.ChannelID,
				CommandID: msg.CommandID,
			}

			if err := s.runSession(ctx, user, im); err != nil {
				log.Err(err)
				return
			}
		}
	}

	if err := s.saveHistory(ctx, msg, platformCmd); err != nil {
		log.Err(err)

		if err := s.messenger.Fail(msg, err.Error()); err != nil {
			log.Err(err)
		}

		return
	}

	user.Session.LastActionTs = time.Now()

	if err := s.messenger.OK(msg); err != nil {
		log.Err(err)
	}
}

// saveHistory posts a command to Platform and add the response link to the message.
func (s *ProcessingService) saveHistory(ctx context.Context, msg *models.Message, platformCmd *platform.Command) error {
	if !s.config.Platform.HistoryEnabled {
		return nil
	}

	commandResponse, err := s.platformManager.PostCommand(ctx, platformCmd)
	if err != nil {
		return errors.Wrap(err, "failed to post a command")
	}

	if commandResponse.CommandLink != "" && platformCmd.Command == CommandExplain {
		msg.AppendText(fmt.Sprintf("Permalink: %s.", commandResponse.CommandLink))

		if err := s.messenger.UpdateText(msg); err != nil {
			// It's not a critical error if we cannot add the link.
			log.Err(err)
		}
	}

	return nil
}

// prepareUserSession sets base properties for the user session according to the incoming message.
func (s *ProcessingService) prepareUserSession(user *usermanager.User, incomingMessage models.IncomingMessage) error {
	if user.Session.ChannelID != "" && user.Session.ChannelID != incomingMessage.ChannelID {
		if err := s.destroySession(user); err != nil {
			return errors.Wrap(err, "failed to destroy old user session")
		}
	}

	user.Session.LastActionTs = time.Now()
	user.Session.ChannelID = incomingMessage.ChannelID
	user.Session.Direct = incomingMessage.Direct

	if user.Session.PlatformSessionID == "" {
		user.Session.PlatformSessionID = incomingMessage.SessionID
	}

	return nil
}

// rebootSession stops a Joe session and creates a new one.
func (s *ProcessingService) rebootSession(msg *models.Message, user *usermanager.User) error {
	msg.AppendText("Session was closed by Database Lab.\n")

	if err := s.messenger.UpdateText(msg); err != nil {
		return errors.Wrapf(err, "failed to append message on session close: %+v", err)
	}

	s.stopSession(user)

	if err := s.runSession(context.TODO(), user, models.IncomingMessage{ChannelID: msg.ChannelID}); err != nil {
		return errors.Wrap(err, "failed to run session")
	}

	return nil
}

// ProcessAppMentionEvent replies to an application mention event.
func (s *ProcessingService) ProcessAppMentionEvent(incomingMessage models.IncomingMessage) {
	msg := models.NewMessage(incomingMessage)

	msg.SetText("What's up? Send `help` to see the list of available commands.")

	if err := s.messenger.Publish(msg); err != nil {
		// TODO(anatoly): Retry.
		log.Err("Bot: Cannot publish a message", err)
		return
	}
}

// Show bot usage hints.
func (s *ProcessingService) showBotHints(incomingMessage models.IncomingMessage, command string, query string) {
	parts := strings.SplitN(query, " ", 2)
	firstQueryWord := strings.ToLower(parts[0])

	checkQuery := len(firstQueryWord) > 0 && command == CommandExec

	if (checkQuery && util.Contains(hintExplainDmlWords, firstQueryWord)) ||
		util.Contains(hintExplainDmlWords, command) {
		msg := models.NewMessage(incomingMessage)
		msg.SetMessageType(models.MessageTypeEphemeral)
		msg.SetUserID(incomingMessage.UserID)
		msg.SetText(HintExplain)

		if err := s.messenger.Publish(msg); err != nil {
			log.Err("Hint explain:", err)
		}
	}

	if util.Contains(hintExecDdlWords, command) {
		msg := models.NewMessage(incomingMessage)
		msg.SetMessageType(models.MessageTypeEphemeral)
		msg.SetUserID(incomingMessage.UserID)
		msg.SetText(HintExec)

		if err := s.messenger.Publish(msg); err != nil {
			log.Err("Hint exec:", err)
		}
	}
}

func (s *ProcessingService) appendHelp(text string) string {
	sb := strings.Builder{}
	entertainerSvc := s.featurePack.Entertainer()

	sb.WriteString(text)
	sb.WriteString(HelpMessage)
	sb.WriteString(entertainerSvc.GetEnterpriseHelpMessage())
	sb.WriteString(fmt.Sprintf("Version: %s (%s)\n", s.config.App.Version, entertainerSvc.GetEdition()))

	return sb.String()
}

// TODO(akartasov): refactor to slice of bytes.
func formatMessage(msg string) string {
	// Slack escapes some characters
	// https://api.slack.com/docs/message-formatting#how_to_escape_characters
	msg = strings.ReplaceAll(msg, "&amp;", "&")
	msg = strings.ReplaceAll(msg, "&lt;", "<")
	msg = strings.ReplaceAll(msg, "&gt;", ">")

	// Smart quotes could be substituted automatically on macOS.
	// Replace smart quotes (“...”) with straight quotes ("...").
	msg = strings.ReplaceAll(msg, "“", "\"")
	msg = strings.ReplaceAll(msg, "”", "\"")
	msg = strings.ReplaceAll(msg, "‘", "'")
	msg = strings.ReplaceAll(msg, "’", "'")

	return msg
}

// parseIncomingMessage extracts received command and query from incoming messages.
func parseIncomingMessage(message string) (string, string) {
	var receivedCommand, query string

	for i, messageRune := range message {
		if unicode.IsSpace(messageRune) {
			// Message: "command query(optional)".
			receivedCommand = message[:i]

			// Extract query and keep the original formatting.
			query = strings.TrimSpace(message[len(receivedCommand):])

			break
		}
	}

	// No spaces found.
	if receivedCommand == "" {
		receivedCommand = message
	}

	return strings.ToLower(receivedCommand), query
}

func appendSessionID(text string, u *usermanager.User) string {
	s := "No session\n"

	if sessionID := getSessionID(u); sessionID != "" {
		s = fmt.Sprintf("Session: `%s`\n", sessionID)
	}

	return text + s
}
