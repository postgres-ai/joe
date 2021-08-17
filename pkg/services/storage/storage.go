/*
2021 © Postgres.ai
*/

// Package storage provides ability to transfer sessions user data to/from memory/disk storage
package storage

import (
	"gitlab.com/postgres-ai/joe/pkg/services/usermanager"
)

// SessionStorage getters and setters for users data.
type SessionStorage interface {
	GetUsers(communicationType string, channelID string) usermanager.UserList
	SetUsers(communicationType string, channelID string, users usermanager.UserList)
}

// PersistentSessionStorage allows to dump data from memory to some persistent storage.
type PersistentSessionStorage interface {
	Load() error
	Save() error

	SessionStorage
}
