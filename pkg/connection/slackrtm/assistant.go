/*
2019 © Postgres.ai
*/

// Package slackrtm provides the Slack implementation of the communication interface.
package slackrtm

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/pkg/errors"
	"github.com/slack-go/slack"

	"gitlab.com/postgres-ai/database-lab/v3/pkg/log"

	"gitlab.com/postgres-ai/joe/features"
	"gitlab.com/postgres-ai/joe/pkg/config"
	"gitlab.com/postgres-ai/joe/pkg/connection"
	"gitlab.com/postgres-ai/joe/pkg/models"
	"gitlab.com/postgres-ai/joe/pkg/services/dblab"
	"gitlab.com/postgres-ai/joe/pkg/services/msgproc"
	"gitlab.com/postgres-ai/joe/pkg/services/platform"
	"gitlab.com/postgres-ai/joe/pkg/services/storage"
	"gitlab.com/postgres-ai/joe/pkg/services/usermanager"
)

var (
	linkRegexp  = regexp.MustCompile(`<http:\/\/[\w.]+\|([.\w]+)>`)
	emailRegexp = regexp.MustCompile(`<mailto:['@\w.]+\|(['@.\w]+)>`)
)

// CommunicationType defines a workspace type.
const CommunicationType = "slackrtm"

// Assistant provides a service for interaction with a communication channel.
type Assistant struct {
	credentialsCfg  *config.Credentials
	procMu          sync.RWMutex
	msgProcessors   map[string]connection.MessageProcessor
	appCfg          *config.Config
	featurePack     *features.Pack
	rtm             *slack.RTM
	messenger       *Messenger
	userInformer    usermanager.UserInformer
	platformManager *platform.Client
	sessionStorage  storage.SessionStorage
}

// SlackConfig defines a slack configuration parameters.
type SlackConfig struct {
	AccessToken string
}

// NewAssistant returns a new assistant service.
func NewAssistant(cfg *config.Credentials, appCfg *config.Config, pack *features.Pack,
	platformClient *platform.Client, sessionStorage storage.SessionStorage) *Assistant {
	slackCfg := &SlackConfig{
		AccessToken: cfg.AccessToken,
	}

	chatAPI := slack.New(slackCfg.AccessToken)

	rtm := chatAPI.NewRTM()

	messenger := NewMessenger(rtm, slackCfg)
	userInformer := NewUserInformer(rtm)

	assistant := &Assistant{
		credentialsCfg:  cfg,
		appCfg:          appCfg,
		msgProcessors:   make(map[string]connection.MessageProcessor),
		featurePack:     pack,
		rtm:             rtm,
		messenger:       messenger,
		userInformer:    userInformer,
		platformManager: platformClient,
		sessionStorage:  sessionStorage,
	}

	return assistant
}

func (a *Assistant) validateCredentials() error {
	if a.credentialsCfg == nil || a.credentialsCfg.AccessToken == "" {
		return errors.New(`"accessToken" must not be empty`)
	}

	return nil
}

// Init initializes assistant handlers.
func (a *Assistant) Init() error {
	if err := a.validateCredentials(); err != nil {
		return errors.Wrap(err, "invalid credentials given")
	}

	return nil
}

// Register registers the assistant service.
func (a *Assistant) Register(ctx context.Context) error {
	go a.rtm.ManageConnection()
	go a.handleRTMEvents(ctx, a.rtm.IncomingEvents)

	return nil
}

// Deregister deregisters the assistant service.
func (a *Assistant) Deregister(_ context.Context) error {
	return nil
}

// AddChannel sets a message processor for a specific channel.
func (a *Assistant) AddChannel(channelID string, dbLabInstance *dblab.Instance) {
	messageProcessor := a.buildMessageProcessor(channelID, dbLabInstance)

	a.addProcessingService(channelID, messageProcessor)
}

func (a *Assistant) buildMessageProcessor(channelID string, dbLabInstance *dblab.Instance) *msgproc.ProcessingService {
	processingCfg := msgproc.ProcessingConfig{
		App:      a.appCfg.App,
		Platform: a.appCfg.Platform,
		DBLab:    dbLabInstance.Config(),
		EntOpts:  a.appCfg.Enterprise,
		Project:  a.appCfg.Platform.Project,
	}

	users := a.sessionStorage.GetUsers(CommunicationType, channelID)
	um := usermanager.NewUserManager(a.userInformer, a.appCfg.Enterprise.Quota, users)

	return msgproc.NewProcessingService(a.messenger, MessageValidator{}, dbLabInstance.Client(), um, a.platformManager,
		processingCfg, a.featurePack)
}

func (a *Assistant) handleRTMEvents(ctx context.Context, incomingEvents chan slack.RTMEvent) {
	for msg := range incomingEvents {
		if ctx.Err() != nil {
			return
		}

		switch ev := msg.Data.(type) {
		case *slack.MessageEvent:
			log.Dbg("Event type: Message")

			if ev.Msg.SubType != "" {
				// Handle only normal messages.
				continue
			}

			if ev.BotID != "" {
				// Skip messages sent by bots.
				continue
			}

			msgProcessor, err := a.getProcessingService(ev.Channel)
			if err != nil {
				log.Err("failed to get processing service", err)
				continue
			}

			msg := a.messageEventToIncomingMessage(ev)
			go msgProcessor.ProcessMessageEvent(ctx, msg)

		case *slack.DesktopNotificationEvent:
			log.Dbg(fmt.Sprintf("Desktop Notification: %v\n", ev))

			msgProcessor, err := a.getProcessingService(ev.Channel)
			if err != nil {
				log.Err("failed to get processing service", err)
				return
			}

			msg := a.desktopNotificationEventToIncomingMessage(ev)
			msgProcessor.ProcessAppMentionEvent(msg)

		case *slack.DisconnectedEvent:
			log.Dbg(fmt.Sprintf("Disconnect event: %v\n", ev.Cause.Error()))

		case *slack.LatencyReport:
			log.Dbg(fmt.Sprintf("Current latency: %v\n", ev.Value))

		default:
			log.Dbg(fmt.Sprintf("Event filtered: skip %q event type", msg.Type))
		}
	}
}

// addProcessingService adds a message processor for a specific channel.
func (a *Assistant) addProcessingService(channelID string, messageProcessor connection.MessageProcessor) {
	a.procMu.Lock()
	a.msgProcessors[channelID] = messageProcessor
	a.procMu.Unlock()
}

// getProcessingService returns processing service by channelID.
func (a *Assistant) getProcessingService(channelID string) (connection.MessageProcessor, error) {
	a.procMu.RLock()
	defer a.procMu.RUnlock()

	messageProcessor, ok := a.msgProcessors[channelID]
	if !ok {
		return nil, errors.Errorf("message processor for %q channel not found", channelID)
	}

	return messageProcessor, nil
}

// RestoreSessions checks sessions after restart and establishes DB connection.
func (a *Assistant) RestoreSessions(ctx context.Context) error {
	log.Dbg("Restore sessions", CommunicationType)

	a.procMu.RLock()
	defer a.procMu.RUnlock()

	for _, proc := range a.msgProcessors {
		if err := proc.RestoreSessions(ctx); err != nil {
			return err
		}
	}

	return nil
}

// CheckIdleSessions check the running user sessions for idleness.
func (a *Assistant) CheckIdleSessions(ctx context.Context) {
	a.procMu.RLock()
	for _, proc := range a.msgProcessors {
		proc.CheckIdleSessions(ctx)
	}
	a.procMu.RUnlock()
}

// desktopNotificationEventToIncomingMessage converts a Slack application mention event to the standard incoming message.
func (a *Assistant) desktopNotificationEventToIncomingMessage(event *slack.DesktopNotificationEvent) models.IncomingMessage {
	inputEvent := models.IncomingMessage{
		Text:      event.Content,
		ChannelID: event.Channel,
		Timestamp: event.Timestamp,
	}

	return inputEvent
}

// messageEventToIncomingMessage converts a Slack message event to the standard incoming message.
func (a *Assistant) messageEventToIncomingMessage(event *slack.MessageEvent) models.IncomingMessage {
	message := unfurlLinks(event.Text)

	inputEvent := models.IncomingMessage{
		SubType:     event.SubType,
		Text:        message,
		ChannelID:   event.Channel,
		ChannelType: event.Type,
		UserID:      event.User,
		Timestamp:   event.Timestamp,
		ThreadID:    event.ThreadTimestamp,
	}

	// Skip messages sent by bots.
	if event.BotID != "" {
		inputEvent.UserID = ""
	}

	files := event.Files
	if len(files) > 0 {
		inputEvent.SnippetURL = files[0].URLPrivate
	}

	return inputEvent
}

// unfurlLinks unfurls Slack links to the original content.
func unfurlLinks(text string) string {
	if strings.Contains(text, "<http:") {
		text = linkRegexp.ReplaceAllString(text, `$1`)
	}

	if strings.Contains(text, "<mailto:") {
		text = emailRegexp.ReplaceAllString(text, `$1`)
	}

	return text
}

// DumpSessions collects user's data from every message processor to sessionStorage.
func (a *Assistant) DumpSessions() {
	log.Dbg("dump sessions", CommunicationType)

	a.procMu.RLock()
	defer a.procMu.RUnlock()

	for channelID, proc := range a.msgProcessors {
		a.sessionStorage.SetUsers(CommunicationType, channelID, proc.Users())
	}
}
