/*
2019 © Postgres.ai
*/

package usermanager

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/pkg/errors"

	"gitlab.com/postgres-ai/joe/features/definition"
	"gitlab.com/postgres-ai/joe/pkg/models"
)

// UserInformer defines an interface for getting user info.
type UserInformer interface {
	GetUserInfo(userID string) (models.UserInfo, error)
}

// UserList represents map of users stored in UserManager.
type UserList map[string]*User // UID -> UserInfo.

// UserManager defines a user manager service.
type UserManager struct {
	UserInformer UserInformer
	QuotaConfig  definition.Quota

	usersMutex    sync.RWMutex
	users         UserList
	sessionScoped bool
	quotas        map[string]*requestQuota
}

// NewUserManager creates a new user manager.
func NewUserManager(informer UserInformer, quotaCfg definition.Quota, users UserList) *UserManager {
	if users == nil {
		users = make(UserList)
	}
	return &UserManager{
		UserInformer: informer,
		QuotaConfig:  quotaCfg,
		users:        users,
	}
}

// NewSessionUserManager isolates Web UI experiments while retaining the real actor.
// Existing on-disk user-keyed sessions are reindexed without replacing their clones.
func NewSessionUserManager(informer UserInformer, quotaCfg definition.Quota, users UserList) *UserManager {
	um := NewUserManager(informer, quotaCfg, nil)
	um.sessionScoped = true
	um.quotas = make(map[string]*requestQuota)

	for _, user := range users {
		if user == nil {
			continue
		}

		if user.Session.ExperimentID == "" {
			user.Session.ExperimentID = user.Session.PlatformSessionID
		}

		key := sessionKey(user.UserInfo.ID, user.Session.ExperimentID)
		um.users[key] = user
		um.attachQuota(user)
	}

	return um
}

func sessionKey(userID, sessionID string) string {
	key, _ := json.Marshal([2]string{userID, sessionID})
	return string(key)
}

func (um *UserManager) attachQuota(user *User) {
	quota := um.quotas[user.UserInfo.ID]
	if quota == nil {
		quota = &requestQuota{quota: Quota{ts: time.Now(), limit: um.QuotaConfig.Limit, interval: um.QuotaConfig.Interval}}
		um.quotas[user.UserInfo.ID] = quota
	}

	user.requestQuota = quota
}

// Users returns a snapshot of the session index.
func (um *UserManager) Users() UserList {
	um.usersMutex.RLock()
	defer um.usersMutex.RUnlock()

	users := make(UserList, len(um.users))
	for key, user := range um.users {
		users[key] = user
	}

	return users
}

// CreateUser returns the actor's session. Only Web UI managers use sessionID.
func (um *UserManager) CreateUser(userID string, sessionIDs ...string) (*User, error) {
	um.usersMutex.Lock()
	defer um.usersMutex.Unlock()

	key := userID
	sessionID := ""

	if um.sessionScoped {
		if len(sessionIDs) != 1 || sessionIDs[0] == "" {
			return nil, errors.New("experiment session ID is required")
		}

		sessionID = sessionIDs[0]
		key = sessionKey(userID, sessionID)
	}

	if user, ok := um.users[key]; ok {
		return user, nil
	}

	chatUser, err := um.UserInformer.GetUserInfo(userID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get user info")
	}

	quota := Quota{ts: time.Now(), limit: um.QuotaConfig.Limit, interval: um.QuotaConfig.Interval}

	user := NewUser(chatUser, quota)
	if um.sessionScoped {
		user.Session.ExperimentID = sessionID
		user.Session.PlatformSessionID = sessionID
		um.attachQuota(user)
	}

	um.users[key] = user

	return user, nil
}
