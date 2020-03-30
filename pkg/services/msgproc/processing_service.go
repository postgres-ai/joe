/*
2019 © Postgres.ai
*/

package msgproc

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"strings"
	"time"

	"github.com/pkg/errors"

	"gitlab.com/postgres-ai/database-lab/pkg/client/dblabapi"
	"gitlab.com/postgres-ai/database-lab/pkg/log"
	"gitlab.com/postgres-ai/database-lab/pkg/util"

	"gitlab.com/postgres-ai/joe/features"
	"gitlab.com/postgres-ai/joe/features/definition"
	"gitlab.com/postgres-ai/joe/pkg/bot/api"
	"gitlab.com/postgres-ai/joe/pkg/bot/command"
	"gitlab.com/postgres-ai/joe/pkg/config"
	"gitlab.com/postgres-ai/joe/pkg/connection"
	"gitlab.com/postgres-ai/joe/pkg/models"
	"gitlab.com/postgres-ai/joe/pkg/pgexplain"
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
	commandBuilder   features.CommandFactoryMethod
	messageValidator connection.MessageValidator
	messenger        connection.Messenger
	DBLab            *dblabapi.Client
	UserManager      *usermanager.UserManager
	config           ProcessingConfig

	// TODO (akartasov): Add specific services.
	//PlatformManager
	//Auditor
	//Limiter
}

// ProcessingConfig declares a configuration of Processing Service.
type ProcessingConfig struct {
	App      config.App
	Platform config.Platform
	Explain  pgexplain.ExplainConfig
	DBLab    config.DBLabInstance
}

var spaceRegex = regexp.MustCompile(`\s+`)

// NewProcessingService creates a new processing service.
func NewProcessingService(messengerSvc connection.Messenger, msgValidator connection.MessageValidator, dblab *dblabapi.Client,
	userSvc *usermanager.UserManager, cfg ProcessingConfig, cmdBuilder features.CommandFactoryMethod) *ProcessingService {
	return &ProcessingService{
		commandBuilder:   cmdBuilder,
		messageValidator: msgValidator,
		messenger:        messengerSvc,
		DBLab:            dblab,
		UserManager:      userSvc,
		config:           cfg,
	}
}

// ProcessMessageEvent replies to a message.
func (s *ProcessingService) ProcessMessageEvent(incomingMessage models.IncomingMessage) {
	// Filter incoming message.
	if err := s.messageValidator.Validate(&incomingMessage); err != nil {
		log.Err(errors.Wrap(err, "incoming message is invalid"))
		return
	}

	// Get user or create a new one.
	user, err := s.UserManager.CreateUser(incomingMessage.UserID)
	if err != nil {
		log.Err(errors.Wrap(err, "failed to get user"))

		if err := s.messenger.Fail(models.NewMessage(incomingMessage.ChannelID), err.Error()); err != nil {
			log.Err(errors.Wrap(err, "failed to get user"))
			return
		}

		return
	}

	user.Session.LastActionTs = time.Now()
	user.Session.ChannelID = incomingMessage.ChannelID

	// Filter and prepare message.
	message := strings.TrimSpace(incomingMessage.Text)
	message = strings.TrimLeft(message, "`")
	message = strings.TrimRight(message, "`")
	message = formatMessage(message)

	// Get command from snippet if exists. Snippets allow longer queries support.
	if incomingMessage.SnippetURL != "" {
		log.Dbg("Using attached file as message")

		snippet, err := s.messenger.DownloadArtifact(incomingMessage.SnippetURL)
		if err != nil {
			log.Err(err)

			if err := s.messenger.Fail(models.NewMessage(incomingMessage.ChannelID), err.Error()); err != nil {
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

	// Replace any number of spaces, tab, new lines with single space.
	message = spaceRegex.ReplaceAllString(message, " ")

	const messageParts = 2

	// Message: "command query(optional)".
	parts := strings.SplitN(message, " ", messageParts)
	receivedCommand := strings.ToLower(parts[0])

	query := ""
	if len(parts) >= messageParts {
		query = parts[1]
	}

	s.showBotHints(incomingMessage, receivedCommand, query)

	if !util.Contains(supportedCommands, receivedCommand) {
		log.Dbg("Message filtered: Not a command")
		return
	}

	if err := user.RequestQuota(); err != nil {
		log.Err("Quota: ", err)

		if err := s.messenger.Fail(models.NewMessage(incomingMessage.ChannelID), err.Error()); err != nil {
			log.Err(errors.Wrap(err, "failed to request quotas"))
			return
		}

		return
	}

	// We want to save message height space for more valuable info.
	queryPreview := strings.ReplaceAll(query, "\n", " ")
	queryPreview = strings.ReplaceAll(queryPreview, "\t", " ")
	queryPreview, _ = text.CutText(queryPreview, QueryPreviewSize, SeparatorEllipsis)

	if s.config.App.AuditEnabled {
		audit, err := json.Marshal(models.Audit{
			ID:       user.UserInfo.ID,
			Name:     user.UserInfo.Name,
			RealName: user.UserInfo.RealName,
			Command:  receivedCommand,
			Query:    query,
		})

		if err != nil {
			if err := s.messenger.Fail(models.NewMessage(incomingMessage.ChannelID), err.Error()); err != nil {
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
		msg := models.NewMessage(incomingMessage.ChannelID)

		// TODO (akartasov): make a separate interface.
		helper := s.commandBuilder(nil, nil, nil, nil)

		msgText = s.appendHelp(helper, msgText)
		msgText = appendSessionID(msgText, user)
		msg.SetText(msgText)

		if err := s.messenger.Publish(msg); err != nil {
			// TODO(anatoly): Retry.
			log.Err("Bot: Cannot publish a message", err)
		}

		return
	}

	if err := s.runSession(context.TODO(), user, incomingMessage.ChannelID); err != nil {
		log.Err(err)
		return
	}

	msg := models.NewMessage(incomingMessage.ChannelID)

	msgText = appendSessionID(msgText, user)
	msg.SetText(msgText)

	if err := s.messenger.Publish(msg); err != nil {
		// TODO(anatoly): Retry.
		log.Err("Bot: Cannot publish a message", err)
		return
	}

	remindDuration := time.Duration(s.config.App.MinNotifyDurationMinutes) * time.Minute
	if err := msg.SetNotifyAt(remindDuration); err != nil {
		log.Err(err)
	}

	msg.SetUserID(user.UserInfo.ID)

	if err := s.messenger.UpdateStatus(msg, models.StatusRunning); err != nil {
		log.Err(err)
	}

	apiCmd := &api.ApiCommand{
		AccessToken: s.config.Platform.Token,
		ApiURL:      s.config.Platform.URL,
		SessionId:   user.Session.PlatformSessionID,
		Command:     receivedCommand,
		Query:       query,
		SlackTs:     incomingMessage.Timestamp,
	}

	const maxRetryCounter = 1

	// TODO(akartasov): Refactor commands and create retrier.
	for iteration := 0; iteration <= maxRetryCounter; iteration++ {
		switch {
		case receivedCommand == CommandExplain:
			err = command.Explain(s.messenger, apiCmd, msg, s.config.Explain, user.Session.CloneConnection)

		case receivedCommand == CommandPlan:
			planCmd := command.NewPlan(apiCmd, msg, user.Session.CloneConnection, s.messenger)
			err = planCmd.Execute()

		case receivedCommand == CommandExec:
			execCmd := command.NewExec(apiCmd, msg, user.Session.CloneConnection, s.messenger)
			err = execCmd.Execute()

		case receivedCommand == CommandReset:
			err = command.ResetSession(context.TODO(), apiCmd, msg, s.DBLab, user.Session.Clone.ID, s.messenger)

		case receivedCommand == CommandHypo:
			hypoCmd := command.NewHypo(apiCmd, msg, user.Session.CloneConnection, s.messenger)
			err = hypoCmd.Execute()

		case receivedCommand == CommandActivity:
			activityCmd := s.commandBuilder(apiCmd, msg, user.Session.CloneConnection, s.messenger).BuildActivityCmd()
			err = activityCmd.Execute()

		case receivedCommand == CommandTerminate:
			terminateCmd := s.commandBuilder(apiCmd, msg, user.Session.CloneConnection, s.messenger).BuildTerminateCmd()
			err = terminateCmd.Execute()

		case util.Contains(allowedPsqlCommands, receivedCommand):
			runner := pgtransmission.NewPgTransmitter(user.Session.ConnParams, pgtransmission.LogsEnabledDefault)
			err = command.Transmit(apiCmd, msg, s.messenger, runner)
		}

		if err != nil {
			if _, ok := err.(*net.OpError); !ok || iteration == maxRetryCounter {
				s.messenger.Fail(msg, err.Error())
				apiCmd.Fail(err.Error())

				return
			}

			if s.isActiveSession(context.TODO(), user.Session.Clone.ID) {
				continue
			}

			msg.AppendText("Session was closed by Database Lab.\n")
			if err := s.messenger.UpdateText(msg); err != nil {
				log.Err(fmt.Sprintf("failed to append message on session close: %+v", err))
			}
			s.stopSession(user)

			if err := s.runSession(context.TODO(), user, msg.ChannelID); err != nil {
				log.Err(err)
				return
			}
		}

		break
	}

	if s.config.Platform.HistoryEnabled {
		if _, err := apiCmd.Post(); err != nil {
			log.Err(err)
			s.messenger.Fail(msg, err.Error())

			return
		}
	}

	if err := s.messenger.OK(msg); err != nil {
		log.Err(err)
	}
}

// ProcessAppMentionEvent replies to an application mention event.
func (s *ProcessingService) ProcessAppMentionEvent(incomingMessage models.IncomingMessage) {
	msg := models.NewMessage(incomingMessage.ChannelID)

	msg.SetText("What's up? Send `help` to see the list of available commands.")

	if err := s.messenger.Publish(msg); err != nil {
		// TODO(anatoly): Retry.
		log.Err("Bot: Cannot publish a message", err)
		return
	}
}

// Show bot usage hints.
func (s *ProcessingService) showBotHints(ev models.IncomingMessage, command string, query string) {
	parts := strings.SplitN(query, " ", 2)
	firstQueryWord := strings.ToLower(parts[0])

	checkQuery := len(firstQueryWord) > 0 && command == CommandExec

	if (checkQuery && util.Contains(hintExplainDmlWords, firstQueryWord)) ||
		util.Contains(hintExplainDmlWords, command) {
		msg := models.NewMessage(ev.ChannelID)
		msg.SetMessageType(models.MessageTypeEphemeral)
		msg.SetUserID(ev.UserID)
		msg.SetText(HintExplain)

		if err := s.messenger.Publish(msg); err != nil {
			log.Err("Hint explain:", err)
		}
	}

	if util.Contains(hintExecDdlWords, command) {
		msg := models.NewMessage(ev.ChannelID)
		msg.SetMessageType(models.MessageTypeEphemeral)
		msg.SetUserID(ev.UserID)
		msg.SetText(HintExec)

		if err := s.messenger.Publish(msg); err != nil {
			log.Err("Hint exec:", err)
		}
	}
}

func (s *ProcessingService) appendHelp(helper definition.EnterpriseHelpMessenger, text string) string {
	sb := strings.Builder{}

	sb.WriteString(text)
	sb.WriteString(HelpMessage)
	sb.WriteString(helper.GetEnterpriseHelpMessage())

	sb.WriteString("Version: ")
	sb.WriteString(s.config.App.Version)
	sb.WriteString("\n")

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

func appendSessionID(text string, u *usermanager.User) string {
	s := "No session\n"

	if u != nil && u.Session.Clone != nil && u.Session.Clone.ID != "" {
		sessionID := u.Session.Clone.ID

		// Use session ID from platform if it's defined.
		if u.Session.PlatformSessionID != "" {
			sessionID = u.Session.PlatformSessionID
		}

		s = fmt.Sprintf("Session: `%s`\n", sessionID)
	}

	return text + s
}
