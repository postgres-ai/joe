package webui

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"gitlab.com/postgres-ai/joe/pkg/config"
)

func TestChannelsAdvertiseExperimentSupport(t *testing.T) {
	a := &Assistant{appCfg: &config.Config{ChannelMapping: &config.ChannelMapping{
		CommunicationTypes: map[string][]config.Workspace{CommunicationType: {{Channels: []config.Channel{{ChannelID: "db"}}}}},
	}}}
	response := httptest.NewRecorder()
	a.channelsHandler(response, httptest.NewRequest("GET", "/channels", nil))
	var channels []struct {
		ChannelID           string `json:"channel_id"`
		MultipleExperiments bool   `json:"multiple_experiments"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &channels); err != nil {
		t.Fatal(err)
	}
	if len(channels) != 1 || channels[0].ChannelID != "db" || !channels[0].MultipleExperiments {
		t.Fatalf("unexpected capabilities: %s", response.Body.String())
	}
}
