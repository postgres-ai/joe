//go:build ee

/*
2026 © Postgres.ai
*/

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestLoadAppConfigResolvesEnterprisePlaceholders asserts the values only the
// enterprise provider can produce. It re-parses the expanded bytes, so this is
// the one test that runs a placeholder through the real production path rather
// than a container declared by the test. The community provider returns fixed
// defaults without reading the config at all, which is why these assertions
// cannot live in the shared test.
func TestLoadAppConfigResolvesEnterprisePlaceholders(t *testing.T) {
	setupTestEnv(t)

	configPath := filepath.Join(t.TempDir(), "joe.yml")
	require.NoError(t, os.WriteFile(configPath, []byte(testConfigYAML), 0600))

	cfg, err := loadAppConfig(configPath)
	require.NoError(t, err)
	require.Equal(t, uint(20), cfg.Enterprise.Quota.Limit)
	require.Equal(t, uint(120), cfg.Enterprise.Quota.Interval)
	require.True(t, cfg.Enterprise.Audit.Enabled)
	require.Equal(t, uint(3), cfg.Enterprise.DBLab.InstanceLimit)
}
