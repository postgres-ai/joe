package bot

import (
	"github.com/pkg/errors"
)

func (b *Bot) createUser(userID string) (*User, error) {
	user, ok := b.findUser(userID)
	if ok {
		return user, nil
	}

	chatUser, err := b.Chat.GetUserInfo(userID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get user info")
	}

	user = NewUser(chatUser, b.Config)

	if err := b.addUser(userID, user); err != nil {
		return nil, errors.Wrap(err, "failed to add user")
	}

	return user, nil
}

func (b *Bot) findUser(userID string) (*User, bool) {
	b.usersMutex.RLock()
	user, ok := b.Users[userID]
	b.usersMutex.RUnlock()

	return user, ok
}

func (b *Bot) addUser(userID string, user *User) error {
	b.usersMutex.Lock()
	defer b.usersMutex.Unlock()

	if _, ok := b.Users[userID]; ok {
		return errors.Errorf("user %q already exists", userID)
	}

	b.Users[userID] = user

	return nil
}
