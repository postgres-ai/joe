package main

import (
	"os"
	"path"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/postgres-ai/joe/pkg/config"
)

const testConfigYAML = `app:
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
`

func setupTestEnv(t *testing.T) {
	t.Helper()
	t.Setenv("JOE_PLATFORM_URL", "https://override.example.com/api/general")
	t.Setenv("PLATFORM_SECRET_TOKEN", "platform-secret")
	t.Setenv("DBLAB_SECRET_TOKEN", "dblab-secret")
	t.Setenv("SLACK_ACCESS_TOKEN", "xoxb-test")
	t.Setenv("SLACK_SIGNING_SECRET", "signing-secret")
}

func TestLoadAppConfigExpandsEnvironmentVariables(t *testing.T) {
	setupTestEnv(t)

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "joe.yml")
	require.NoError(t, os.WriteFile(configPath, []byte(testConfigYAML), 0600))

	cfg, err := loadAppConfig(configPath)
	require.NoError(t, err)
	require.Equal(t, "https://override.example.com/api/general", cfg.Platform.URL)
	require.Equal(t, "platform-secret", cfg.Platform.Token)
	require.Equal(t, "dblab-secret", cfg.ChannelMapping.DBLabInstances["prod1"].Token)
	require.Len(t, cfg.ChannelMapping.CommunicationTypes["slack"], 1)
	require.Equal(t, "xoxb-test", cfg.ChannelMapping.CommunicationTypes["slack"][0].Credentials.AccessToken)
	require.Equal(t, "signing-secret", cfg.ChannelMapping.CommunicationTypes["slack"][0].Credentials.SigningSecret)
}

// TestLoadAppConfigUsesProductionPath exercises the same path.Join(...) that
// main() uses, so a typo or value change in ConfigsPath/AppFilename surfaces
// in CI rather than at runtime.
func TestLoadAppConfigUsesProductionPath(t *testing.T) {
	setupTestEnv(t)

	tmpDir := t.TempDir()
	oldCwd, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chdir(oldCwd) })
	require.NoError(t, os.Chdir(tmpDir))
	require.NoError(t, os.Mkdir(config.ConfigsPath, 0o700))

	configPath := path.Join(config.ConfigsPath, config.AppFilename)
	require.NoError(t, os.WriteFile(configPath, []byte(testConfigYAML), 0o600))

	cfg, err := loadAppConfig(configPath)
	require.NoError(t, err)
	require.Equal(t, "platform-secret", cfg.Platform.Token)
}

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*config.Config)
		wantErr string
	}{
		{
			name:   "no platform integration, empty token is fine",
			mutate: func(c *config.Config) { c.Platform.Token = "" },
		},
		{
			name: "history enabled requires token",
			mutate: func(c *config.Config) {
				c.Platform.Token = ""
				c.Platform.HistoryEnabled = true
			},
			wantErr: "platform.token",
		},
		{
			name: "registration enabled requires token",
			mutate: func(c *config.Config) {
				c.Platform.Token = ""
				c.Registration.Enable = true
				c.Platform.Project = "demo"
			},
			wantErr: "platform.token",
		},
		{
			name: "registration enabled requires project",
			mutate: func(c *config.Config) {
				c.Platform.Token = "set"
				c.Registration.Enable = true
				c.Platform.Project = ""
			},
			wantErr: "platform.project",
		},
		{
			name: "registration enabled with both fields ok",
			mutate: func(c *config.Config) {
				c.Platform.Token = "set"
				c.Registration.Enable = true
				c.Platform.Project = "demo"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Config{}
			tt.mutate(&cfg)
			err := validateConfig(&cfg)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
