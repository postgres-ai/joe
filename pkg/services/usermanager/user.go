/*
2019 © Postgres.ai
*/

// Package usermanager provides a service for user management.
package usermanager

import (
	"time"

	"github.com/dustin/go-humanize/english"
	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/pkg/errors"

	dblabmodels "gitlab.com/postgres-ai/database-lab/pkg/models"

	"gitlab.com/postgres-ai/joe/pkg/models"
	"gitlab.com/postgres-ai/joe/pkg/util"
)

// User defines user info and session.
type User struct {
	UserInfo models.UserInfo
	Session  UserSession
}

// UserSession defines a user session.
type UserSession struct {
	PlatformSessionID string
	ChannelID         string
	Direct            bool

	Quota Quota

	LastActionTs time.Time
	IdleInterval uint

	Clone           *dblabmodels.Clone
	ConnParams      models.Clone
	CloneConnection *pgxpool.Pool
}

// Quota defines a user quota for requests.
type Quota struct {
	ts       time.Time
	count    uint
	limit    uint
	interval uint
}

// NewUser creates a new User.
func NewUser(userInfo models.UserInfo, quota Quota) *User {
	ts := time.Now()

	user := User{
		UserInfo: userInfo,
		Session: UserSession{
			Quota:        quota,
			LastActionTs: ts,
		},
	}

	return &user
}

// RequestQuota checks a user request limit.
func (u *User) RequestQuota() error {
	limit := u.Session.Quota.limit
	interval := u.Session.Quota.interval
	sAgo := util.SecondsAgo(u.Session.Quota.ts)

	if sAgo < interval {
		if u.Session.Quota.count >= limit {
			return errors.Errorf("You have reached the limit of requests per %s (%d). Please wait before trying again",
				english.Plural(int(interval), "second", ""), limit)
		}

		u.Session.Quota.count++
		return nil
	}

	u.Session.Quota.count = 1
	u.Session.Quota.ts = time.Now()

	return nil
}
