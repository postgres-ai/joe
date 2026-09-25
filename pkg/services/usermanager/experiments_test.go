package usermanager

import (
	"encoding/json"
	"sync"
	"testing"

	"gitlab.com/postgres-ai/joe/features/definition"
	"gitlab.com/postgres-ai/joe/pkg/models"
)

type experimentInformer struct{}

func (experimentInformer) GetUserInfo(id string) (models.UserInfo, error) {
	return models.UserInfo{ID: id}, nil
}

func TestExperimentsAreIndependentAndKeepActor(t *testing.T) {
	um := NewSessionUserManager(experimentInformer{}, definition.Quota{Limit: 2, Interval: 3600}, nil)
	a, err := um.CreateUser("alice", "experiment-a")
	if err != nil {
		t.Fatal(err)
	}
	b, err := um.CreateUser("alice", "experiment-b")
	if err != nil {
		t.Fatal(err)
	}
	if a == b || a.UserInfo.ID != "alice" || b.UserInfo.ID != "alice" {
		t.Fatal("experiments must isolate state, not impersonate actors")
	}
	a.Session.ChannelID = "channel-a"
	if b.Session.ChannelID != "" {
		t.Fatal("session state leaked")
	}
	again, _ := um.CreateUser("alice", "experiment-a")
	if again != a {
		t.Fatal("existing experiment was replaced")
	}
	other, _ := um.CreateUser("bob", "experiment-a")
	if other == a {
		t.Fatal("cross-user session reuse")
	}
	if a.RequestQuota() != nil || b.RequestQuota() != nil || a.RequestQuota() == nil {
		t.Fatal("request quota must be shared across experiments")
	}
	if _, err := um.CreateUser("alice"); err == nil {
		t.Fatal("missing experiment ID accepted")
	}
}

func TestRestoreLegacyAndRoundTripExperiments(t *testing.T) {
	legacy := NewUser(models.UserInfo{ID: "alice"}, Quota{})
	legacy.Session.PlatformSessionID = "old-session"
	um := NewSessionUserManager(experimentInformer{}, definition.Quota{}, UserList{"alice": legacy})
	restored, _ := um.CreateUser("alice", "old-session")
	if restored != legacy {
		t.Fatal("legacy session lost")
	}
	b, _ := um.CreateUser("alice", "new-session")
	// Idle cleanup clears the live platform session, but the experiment key must survive.
	b.Session.PlatformSessionID = ""
	data, err := json.Marshal(um.Users())
	if err != nil {
		t.Fatal(err)
	}
	var users UserList
	if err := json.Unmarshal(data, &users); err != nil {
		t.Fatal(err)
	}
	recovered := NewSessionUserManager(experimentInformer{}, definition.Quota{}, users)
	if len(recovered.Users()) != 2 {
		t.Fatal("lost restored sessions")
	}
	recovered.CreateUser("alice", "new-session")
	if len(recovered.Users()) != 2 {
		t.Fatal("idle experiment key lost after restart")
	}
}

func TestConcurrentExperimentCreation(t *testing.T) {
	um := NewSessionUserManager(experimentInformer{}, definition.Quota{}, nil)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := um.CreateUser("alice", "a"); err != nil {
				t.Error(err)
			}
			um.Users()
		}()
	}
	wg.Wait()
	if len(um.Users()) != 1 {
		t.Fatal("duplicate experiments")
	}
}

func TestChatManagerRemainsSingleSession(t *testing.T) {
	um := NewUserManager(experimentInformer{}, definition.Quota{}, nil)
	a, _ := um.CreateUser("alice", "one")
	b, _ := um.CreateUser("alice", "two")
	if a != b {
		t.Fatal("chat behavior changed")
	}
}
