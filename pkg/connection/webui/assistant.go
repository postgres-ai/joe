/*
2019 © Postgres.ai
*/

// Package webui provides the Web-UI implementation of the communication interface.
package webui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/pkg/errors"

	"gitlab.com/postgres-ai/database-lab/v2/pkg/log"

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

// CommunicationType defines a workspace type.
const CommunicationType = "webui"

// Assistant provides a service for interaction with a communication channel.
type Assistant struct {
	credentialsCfg *config.Credentials
	procMu         sync.RWMutex
	msgProcessors  map[string]connection.MessageProcessor
	appCfg         *config.Config
	featurePack    *features.Pack
	messenger      *Messenger
	userInformer   usermanager.UserInformer
	platformClient *platform.Client
	meta           meta

	sessionStorage storage.SessionStorage
}

// meta contains meta information about an assistant service.
type meta struct {
	instanceID uint64
	prefix     string
}

// NewAssistant returns a new assistant service.
func NewAssistant(cfg *config.Credentials, appCfg *config.Config, handlerPrefix string, pack *features.Pack,
	platformClient *platform.Client, sessionStorage storage.SessionStorage) *Assistant {
	prefix := fmt.Sprintf("/%s", strings.Trim(handlerPrefix, "/"))

	messenger := NewMessenger(platformClient)
	userInformer := NewUserInformer()

	assistant := &Assistant{
		credentialsCfg: cfg,
		appCfg:         appCfg,
		msgProcessors:  make(map[string]connection.MessageProcessor),
		featurePack:    pack,
		messenger:      messenger,
		userInformer:   userInformer,
		platformClient: platformClient,
		sessionStorage: sessionStorage,
		meta:           meta{prefix: prefix},
	}

	return assistant
}

func (a *Assistant) validateCredentials() error {
	if a.credentialsCfg == nil || a.credentialsCfg.SigningSecret == "" {
		return errors.New(`"signingSecret" must not be empty`)
	}

	return nil
}

// Init registers assistant handlers.
func (a *Assistant) Init() error {
	if err := a.validateCredentials(); err != nil {
		return errors.Wrap(err, "invalid credentials given")
	}

	if a.lenMessageProcessor() == 0 {
		return errors.New("no message processor set")
	}

	verifier := NewVerifier([]byte(a.credentialsCfg.SigningSecret))

	for path, handleFunc := range a.handlers() {
		http.Handle(fmt.Sprintf("%s/%s", a.meta.prefix, path), verifier.Handler(handleFunc))
	}

	return nil
}

// Register registers the assistant on the Platform.
func (a *Assistant) Register(ctx context.Context, project string) error {
	log.Dbg(fmt.Sprintf("Assistant %s. Project: %q. URL-path prefix: %s", CommunicationType, project, a.meta.prefix))

	if !a.appCfg.Registration.Enable {
		log.Msg("Auto-registration disabled. To enable it, use the application configuration file")
		return nil
	}

	platformToken, err := a.platformClient.CheckPlatformToken(ctx, platform.TokenCheckRequest{Token: a.appCfg.Platform.Token})
	if err != nil {
		return errors.Wrap(err, "failed to check token")
	}

	registerRequest := platform.RegisterApplicationRequest{
		URL:     a.appCfg.Registration.PublicURL,
		Project: project,
		OrgID:   platformToken.OrganizationID,
		Token:   a.credentialsCfg.SigningSecret,
	}

	instanceID, err := a.platformClient.RegisterApplication(ctx, registerRequest)
	if err != nil {
		return errors.Wrap(err, "failed to register on Platform")
	}

	a.meta.instanceID = instanceID

	return err
}

// Deregister deregisters the assistant from the Platform.
func (a *Assistant) Deregister(ctx context.Context) error {
	if !a.appCfg.Registration.Enable || a.meta.instanceID == 0 {
		log.Dbg("The assistant is not registered. Skip deregistration")
		return nil
	}

	return a.platformClient.DeregisterApplication(ctx, platform.DeregisterApplicationRequest{InstanceID: a.meta.instanceID})
}

// AddChannel sets a message processor for a specific channel.
func (a *Assistant) AddChannel(channelID, project string, dbLabInstance *dblab.Instance) {
	messageProcessor := a.buildMessageProcessor(channelID, project, dbLabInstance)

	a.addProcessingService(channelID, messageProcessor)
}

func (a *Assistant) buildMessageProcessor(channelID, project string, dbLabInstance *dblab.Instance) *msgproc.ProcessingService {
	processingCfg := msgproc.ProcessingConfig{
		App:      a.appCfg.App,
		Platform: a.appCfg.Platform,
		Explain:  a.appCfg.Explain,
		DBLab:    dbLabInstance.Config(),
		EntOpts:  a.appCfg.Enterprise,
		Project:  project,
	}

	users := a.sessionStorage.GetUsers(CommunicationType, channelID)
	um := usermanager.NewUserManager(a.userInformer, a.appCfg.Enterprise.Quota, users)

	return msgproc.NewProcessingService(a.messenger, MessageValidator{}, dbLabInstance.Client(), um, a.platformClient,
		processingCfg, a.featurePack)
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
	log.Dbg("Check idle sessions", a.meta.prefix)

	a.procMu.RLock()
	for _, proc := range a.msgProcessors {
		proc.CheckIdleSessions(ctx)
	}
	a.procMu.RUnlock()
}

func (a *Assistant) lenMessageProcessor() int {
	a.procMu.RLock()
	defer a.procMu.RUnlock()

	return len(a.msgProcessors)
}

func (a *Assistant) handlers() map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"verify":   a.verificationHandler,
		"channels": a.channelsHandler,
		"command":  a.commandHandler,
	}
}

type challengeResponse struct {
	Challenge string `json:"challenge"`
}

func (a *Assistant) verificationHandler(w http.ResponseWriter, r *http.Request) {
	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(r.Body); err != nil {
		log.Err("Failed to read the request body:", err)
		w.WriteHeader(http.StatusBadRequest)

		return
	}

	var resp challengeResponse

	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		log.Err("Challenge parse error:", err)
		w.WriteHeader(http.StatusInternalServerError)

		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (a *Assistant) channelsHandler(w http.ResponseWriter, r *http.Request) {
	channels := []config.Channel{}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	work, ok := a.appCfg.ChannelMapping.CommunicationTypes[CommunicationType]

	// For now, we will use only the first entry in the config.
	if !ok || len(work) == 0 {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	channels = append(channels, work[0].Channels...)

	if err := json.NewEncoder(w).Encode(channels); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

// Message represents commands coming from Platform.
type Message struct {
	SessionID string `json:"session_id"`
	CommandID string `json:"command_id"`
	Text      string `json:"text"`
	ChannelID string `json:"channel_id"`
	UserID    string `json:"user_id"`
	Timestamp string `json:"timestamp"`
}

// ToIncomingMessage converts a WebUI message event to the standard incoming message.
func (m *Message) ToIncomingMessage() models.IncomingMessage {
	incomingMessage := models.IncomingMessage{
		Text:      m.Text,
		ChannelID: m.ChannelID,
		UserID:    m.UserID,
		Timestamp: m.Timestamp,
		CommandID: m.CommandID,
		SessionID: m.SessionID,
		Direct:    true,
	}

	return incomingMessage
}

func (a *Assistant) commandHandler(w http.ResponseWriter, r *http.Request) {
	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(r.Body); err != nil {
		log.Err("Failed to read the request body:", err)
		w.WriteHeader(http.StatusBadRequest)

		return
	}

	body := buf.Bytes()

	webMessage := Message{}
	if err := json.Unmarshal(body, &webMessage); err != nil {
		log.Err("Failed to unmarshal the request body:", err)
		w.WriteHeader(http.StatusBadRequest)

		return
	}

	svc, err := a.getProcessingService(webMessage.ChannelID)
	if err != nil {
		log.Err("Failed to get a processing service", err)
		w.WriteHeader(http.StatusBadRequest)

		return
	}

	go svc.ProcessMessageEvent(context.TODO(), webMessage.ToIncomingMessage())
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
