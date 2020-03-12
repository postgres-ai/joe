/*
2019 © Postgres.ai
*/

package usermanager

import (
	"sync"
	"time"

	"github.com/pkg/errors"

	"gitlab.com/postgres-ai/joe/pkg/config"
	"gitlab.com/postgres-ai/joe/pkg/models"
)

// UserInformer defines an interface for getting user info.
type UserInformer interface {
	GetUserInfo(userID string) (models.UserInfo, error)
}

// UserManager defines a user manager service.
type UserManager struct {
	UserInformer UserInformer
	QuotaConfig  config.Quota

	usersMutex sync.RWMutex
	users      map[string]*User // UID -> UserInfo.
}

// NewUserManager creates a new user manager.
func NewUserManager(informer UserInformer, quotaCfg config.Quota) *UserManager {
	return &UserManager{
		UserInformer: informer,
		QuotaConfig:  quotaCfg,
		users:        make(map[string]*User),
	}
}

// Users returns all users.
func (um *UserManager) Users() map[string]*User {
	return um.users
}

// CreateUser creates a new user.
func (um *UserManager) CreateUser(userID string) (*User, error) {
	user, ok := um.findUser(userID)
	if ok {
		return user, nil
	}

	chatUser, err := um.UserInformer.GetUserInfo(userID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get user info")
	}

	quota := Quota{
		ts:       time.Now(),
		limit:    um.QuotaConfig.Limit,
		interval: um.QuotaConfig.Interval,
	}

	user = NewUser(chatUser, quota)

	if err := um.addUser(userID, user); err != nil {
		return nil, errors.Wrap(err, "failed to add user")
	}

	return user, nil
}

func (um *UserManager) findUser(userID string) (*User, bool) {
	um.usersMutex.RLock()
	user, ok := um.users[userID]
	um.usersMutex.RUnlock()

	return user, ok
}

func (um *UserManager) addUser(userID string, user *User) error {
	um.usersMutex.Lock()
	defer um.usersMutex.Unlock()

	if _, ok := um.users[userID]; ok {
		return errors.Errorf("user %q already exists", userID)
	}

	um.users[userID] = user

	return nil
}
