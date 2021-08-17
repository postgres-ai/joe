package storage

import (
	"encoding/json"
	"io/ioutil"
	"os"

	"github.com/pkg/errors"

	"gitlab.com/postgres-ai/joe/pkg/services/usermanager"
)

// JSONSessionStorage stores user session in file in json format.
type JSONSessionStorage struct {
	usersByChannel map[string]usermanager.UserList // map[assistent-channelID]map[userID]User

	filePath string
}

// NewJSONSessionData creates new storage.
func NewJSONSessionData(filePath string) *JSONSessionStorage {
	usersByChannel := make(map[string]usermanager.UserList)

	return &JSONSessionStorage{
		filePath:       filePath,
		usersByChannel: usersByChannel,
	}
}

// Load reads sessions data from disk.
func (ss *JSONSessionStorage) Load() error {
	ss.usersByChannel = make(map[string]usermanager.UserList)

	data, err := ioutil.ReadFile(ss.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			// no sessions data, ignore
			return nil
		}

		return errors.Wrap(err, "failed to read sessions data")
	}

	return json.Unmarshal(data, &ss.usersByChannel)
}

// Save writes sessions data to disk.
func (ss *JSONSessionStorage) Save() error {
	data, err := json.Marshal(ss.usersByChannel)
	if err != nil {
		return errors.Wrap(err, "failed to encode session data")
	}

	return ioutil.WriteFile(ss.filePath, data, 0600)
}

// GetUsers returns UserList for given message processor.
func (ss *JSONSessionStorage) GetUsers(communicationType string, channelID string) usermanager.UserList {
	id := communicationType + "-" + channelID
	return ss.usersByChannel[id]
}

// SetUsers sets UserList for given message processor.
func (ss *JSONSessionStorage) SetUsers(communicationType string, channelID string, users usermanager.UserList) {
	id := communicationType + "-" + channelID
	ss.usersByChannel[id] = users
}
