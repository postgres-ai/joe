package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadAppConfigExpandsEnvironmentVariables(t *testing.T) {
	oldCwd, err := os.Getwd()
	require.NoError(t, err)
	defer func() {
		require.NoError(t, os.Chdir(oldCwd))
	}()

	tmpDir := t.TempDir()
	require.NoError(t, os.Chdir(tmpDir))
	require.NoError(t, os.Mkdir("configs", 0700))

	t.Setenv("JOE_PLATFORM_URL", "https://override.example.com/api/general")
	t.Setenv("PLATFORM_SECRET_TOKEN", "platform-secret")
	t.Setenv("DBLAB_SECRET_TOKEN", "dblab-secret")
	t.Setenv("SLACK_ACCESS_TOKEN", "xoxb-test")
	t.Setenv("SLACK_SIGNING_SECRET", "signing-secret")

	configPath := filepath.Join(tmpDir, "configs", "joe.yml")
	configData := []byte(`app:
  debug: false
platform:
  url: "https://postgres.ai/api/general"
  token: "${PLATFORM_SECRET_TOKEN}"
  project: "demo"
  historyEnabled: true
registration:
  enable: false
channelMapping:
  dblabServers:
    prod1:
      url: "https://dblab.example.com"
      token: "${DBLAB_SECRET_TOKEN}"
  communicationTypes:
    slack:
      - name: Workspace
        credentials:
          accessToken: "${SLACK_ACCESS_TOKEN}"
          signingSecret: "${SLACK_SIGNING_SECRET}"
        channels:
          - channelID: C123
            dblabServer: prod1
            dblabParams:
              dbname: postgres
              sslmode: prefer
`)
	require.NoError(t, os.WriteFile(configPath, configData, 0600))

	cfg, err := loadAppConfig()
	require.NoError(t, err)
	require.Equal(t, "https://override.example.com/api/general", cfg.Platform.URL)
	require.Equal(t, "platform-secret", cfg.Platform.Token)
	require.Equal(t, "dblab-secret", cfg.ChannelMapping.DBLabInstances["prod1"].Token)
	require.Len(t, cfg.ChannelMapping.CommunicationTypes["slack"], 1)
	require.Equal(t, "xoxb-test", cfg.ChannelMapping.CommunicationTypes["slack"][0].Credentials.AccessToken)
	require.Equal(t, "signing-secret", cfg.ChannelMapping.CommunicationTypes["slack"][0].Credentials.SigningSecret)
}
