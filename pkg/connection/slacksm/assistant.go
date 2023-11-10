/*
2019 © Postgres.ai
*/

// Package slacksm provides the Slack SocketMode implementation of the communication interface.
package slacksm

import (
	"context"
	"fmt"
	"sync"

	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"

	"github.com/pkg/errors"
	"github.com/slack-go/slack"

	"gitlab.com/postgres-ai/database-lab/v2/pkg/log"

	"gitlab.com/postgres-ai/joe/features"
	"gitlab.com/postgres-ai/joe/pkg/config"
	"gitlab.com/postgres-ai/joe/pkg/connection"
	slackConnect "gitlab.com/postgres-ai/joe/pkg/connection/slack"
	"gitlab.com/postgres-ai/joe/pkg/services/dblab"
	"gitlab.com/postgres-ai/joe/pkg/services/msgproc"
	"gitlab.com/postgres-ai/joe/pkg/services/platform"
	"gitlab.com/postgres-ai/joe/pkg/services/storage"
	"gitlab.com/postgres-ai/joe/pkg/services/usermanager"
)

// CommunicationType defines a workspace type.
const CommunicationType = "slacksm"

// Assistant provides a service for interaction with a communication channel.
type Assistant struct {
	credentialsCfg  *config.Credentials
	procMu          sync.RWMutex
	msgProcessors   map[string]connection.MessageProcessor
	appCfg          *config.Config
	featurePack     *features.Pack
	api             *slack.Client
	client          *socketmode.Client
	messenger       *slackConnect.Messenger
	userInformer    usermanager.UserInformer
	platformManager *platform.Client
	sessionStorage  storage.SessionStorage
}

// Config defines a slack configuration parameters.
type Config struct {
	AccessToken   string
	AppLevelToken string
}

// NewAssistant returns a new assistant service.
func NewAssistant(cfg *config.Credentials, appCfg *config.Config, pack *features.Pack,
	platformClient *platform.Client, sessionStorage storage.SessionStorage) *Assistant {
	slackCfg := &Config{
		AccessToken:   cfg.AccessToken,
		AppLevelToken: cfg.AppLevelToken,
	}

	api := slack.New(slackCfg.AccessToken,
		slack.OptionAppLevelToken(cfg.AppLevelToken),
	)

	client := socketmode.New(api)

	messenger := slackConnect.NewMessenger(api, &slackConnect.MessengerConfig{
		AccessToken: slackCfg.AccessToken,
	})

	assistant := &Assistant{
		credentialsCfg:  cfg,
		appCfg:          appCfg,
		msgProcessors:   make(map[string]connection.MessageProcessor),
		featurePack:     pack,
		api:             api,
		client:          client,
		messenger:       messenger,
		userInformer:    slackConnect.NewUserInformer(api),
		platformManager: platformClient,
		sessionStorage:  sessionStorage,
	}

	return assistant
}

func (a *Assistant) validateCredentials() error {
	if a.credentialsCfg == nil || a.credentialsCfg.AccessToken == "" || a.credentialsCfg.AppLevelToken == "" {
		return errors.New(`"accessToken" and "appLevelToken" must not be empty`)
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
	if _, err := a.api.AuthTestContext(ctx); err != nil {
		return errors.Wrap(err, "failed to perform slack auth test")
	}

	// Fail fast to ensure the slack app is properly configured.
	if _, _, err := a.api.StartSocketModeContext(ctx); err != nil {
		return errors.Wrap(err, "failed to init slack socket mode")
	}

	go func() {
		if err := a.client.RunContext(ctx); err != nil && err != context.Canceled {
			log.Errf("failed to run slack SocketMode assistant: %s", err)
		}
	}()

	go a.handleSocketEvents(ctx, a.client.Events)

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

	userList := a.sessionStorage.GetUsers(CommunicationType, channelID)
	userManager := usermanager.NewUserManager(a.userInformer, a.appCfg.Enterprise.Quota, userList)

	return msgproc.NewProcessingService(
		a.messenger,
		slackConnect.MessageValidator{},
		dbLabInstance.Client(),
		userManager,
		a.platformManager,
		processingCfg,
		a.featurePack,
	)
}

func (a *Assistant) handleSocketEvents(ctx context.Context, incomingEvents chan socketmode.Event) {
	var evt socketmode.Event

	for {
		select {
		case <-ctx.Done():
			return
		case evt = <-incomingEvents:
		}

		switch evt.Type {
		case socketmode.EventTypeConnecting:
			log.Dbg("Connecting to Slack with Socket Mode...")
		case socketmode.EventTypeConnectionError:
			log.Dbg("Connection failed. Retrying later...")
		case socketmode.EventTypeConnected:
			log.Dbg("Connected to Slack with Socket Mode.")
		case socketmode.EventTypeEventsAPI:
			eventsAPIEvent, ok := evt.Data.(slackevents.EventsAPIEvent)
			if !ok {
				log.Dbg(fmt.Sprintf("Ignored %+v", evt))
				continue
			}

			log.Dbg(fmt.Sprintf("Event %s received: %+v", eventsAPIEvent.Type, eventsAPIEvent.InnerEvent.Type))

			if evt.Request != nil {
				a.client.Ack(*evt.Request)
			}

			switch eventsAPIEvent.Type {
			case slackevents.CallbackEvent:
				switch ev := eventsAPIEvent.InnerEvent.Data.(type) {
				case *slackevents.MessageEvent:
					a.handleMessageEvent(ctx, ev)

				case *slackevents.AppMentionEvent:
					a.handleAppMentionEvent(ctx, ev)

				default:
					log.Dbg("Ignore message event: ", eventsAPIEvent.InnerEvent.Type)
				}

			default:
				log.Dbg("unsupported Events API event received")
			}

		default:
			log.Dbg("Ignore event type: ", evt.Type)
		}
	}
}

func (a *Assistant) handleMessageEvent(ctx context.Context, ev *slackevents.MessageEvent) {
	if ev.SubType != "" {
		// Handle only normal messages.
		return
	}

	if ev.BotID != "" {
		// Skip messages sent by bots.
		return
	}

	msgProcessor, err := a.getProcessingService(ev.Channel)
	if err != nil {
		log.Err("failed to get processing service", err)
		return
	}

	msg := slackConnect.MessageEventToIncomingMessage(ev)
	go msgProcessor.ProcessMessageEvent(ctx, msg)
}

func (a *Assistant) handleAppMentionEvent(_ context.Context, ev *slackevents.AppMentionEvent) {
	log.Dbg(fmt.Sprintf("Desktop Notification: %v\n", ev))

	msgProcessor, err := a.getProcessingService(ev.Channel)
	if err != nil {
		log.Err("failed to get processing service", err)
		return
	}

	msg := slackConnect.AppMentionEventToIncomingMessage(ev)
	msgProcessor.ProcessAppMentionEvent(msg)
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

// CheckIdleSessions check the running user sessions for idleness.
func (a *Assistant) CheckIdleSessions(ctx context.Context) {
	a.procMu.RLock()
	for _, proc := range a.msgProcessors {
		proc.CheckIdleSessions(ctx)
	}
	a.procMu.RUnlock()
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

// DumpSessions collects user's data from every message processor to sessionStorage.
func (a *Assistant) DumpSessions() {
	log.Dbg("Dump sessions", CommunicationType)

	a.procMu.RLock()
	defer a.procMu.RUnlock()

	for channelID, proc := range a.msgProcessors {
		a.sessionStorage.SetUsers(CommunicationType, channelID, proc.Users())
	}
}
