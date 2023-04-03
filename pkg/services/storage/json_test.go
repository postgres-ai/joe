package storage

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestJSONSessionStorage(t *testing.T) {
	t.Run("it shouldn't panic if sessions file is absent", func(t *testing.T) {
		s := NewJSONSessionData("some-missing-filepath.json")
		err := s.Load()
		assert.NoError(t, err)
	})

	t.Run("it loads sessions.json", func(t *testing.T) {
		filepath, err := prepareSessionJSON()
		assert.NoError(t, err)

		s := NewJSONSessionData(filepath)
		err = s.Load()
		assert.NoError(t, err)

		t.Run("it should return users for known channel", func(t *testing.T) {
			users := s.GetUsers("slackrtm", "CXXXXXXXX")
			assert.NotEmpty(t, users)
			assert.Contains(t, users, "UXXXXXXXXXP")
		})

		t.Run("it shouldn't panic on unknown channel", func(t *testing.T) {
			users := s.GetUsers("missing-type", "CYYYYYYYY")
			assert.Empty(t, users)
		})
	})
}

func prepareSessionJSON() (string, error) {
	data := `
{
  "slackrtm-CXXXXXXXX": {
    "UXXXXXXXXXP": {
      "UserInfo": {
        "ID": "UXXXXXXXXXP",
        "Name": "test",
        "RealName": "Joe Test"
      },
      "Session": {
        "PlatformSessionID": "",
        "ChannelID": "CXXXXXXXX",
        "Direct": false,
        "Quota": {},
        "LastActionTs": "2021-07-08T19:06:52.480340854Z",
        "IdleInterval": 0,
        "Clone": null,
        "ConnParams": {
          "Name": "",
          "Host": "",
          "Port": "",
          "Username": "",
          "Password": "",
          "SSLMode": ""
        }
      }
    }
  },
  "webui-ProductionDB": {
  }
}`
	f, err := os.CreateTemp("", "joe-json-test")
	if err != nil {
		return "", err
	}

	if _, err := f.WriteString(data); err != nil {
		return "", err
	}

	return f.Name(), f.Close()
}
