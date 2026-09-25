/*
2019 © Postgres.ai
*/

// Package usermanager provides a service for user management.
package usermanager

import (
	"sync"
	"time"

	"github.com/dustin/go-humanize/english"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pkg/errors"

	dblabmodels "gitlab.com/postgres-ai/database-lab/v3/pkg/models"

	"gitlab.com/postgres-ai/joe/pkg/models"
	"gitlab.com/postgres-ai/joe/pkg/util"
)

// User defines user info and session.
type User struct {
	UserInfo     models.UserInfo
	Session      UserSession
	requestQuota *requestQuota
	quotaOnce    sync.Once
}

// UserSession defines a user session.
type UserSession struct {
	ExperimentID      string
	PlatformSessionID string
	ChannelID         string
	Direct            bool

	Quota Quota

	LastActionTs time.Time
	IdleInterval uint

	Clone           *dblabmodels.Clone
	ConnParams      models.Clone
	Pool            *pgxpool.Pool `json:"-"`
	CloneConnection *pgx.Conn     `json:"-"`
	DBVersion       int           `json:"-"`
}

type requestQuota struct {
	mu    sync.Mutex
	quota Quota
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
		UserInfo:     userInfo,
		requestQuota: &requestQuota{quota: quota},
		Session: UserSession{
			Quota:        quota,
			LastActionTs: ts,
		},
	}

	return &user
}

// RequestQuota checks a user request limit.
func (u *User) RequestQuota() error {
	u.quotaOnce.Do(func() {
		if u.requestQuota == nil {
			u.requestQuota = &requestQuota{quota: u.Session.Quota}
		}
	})

	u.requestQuota.mu.Lock()
	defer u.requestQuota.mu.Unlock()

	q := &u.requestQuota.quota
	limit := q.limit
	interval := q.interval
	sAgo := util.SecondsAgo(q.ts)

	if sAgo < interval {
		if q.count >= limit {
			return errors.Errorf("You have reached the limit of requests per %s (%d). Please wait before trying again",
				english.Plural(int(interval), "second", ""), limit)
		}

		q.count++
		return nil
	}

	q.count = 1
	q.ts = time.Now()

	return nil
}
