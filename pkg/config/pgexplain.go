/*
2019 © Postgres.ai
*/

package config

import (
	"os"
	"path"
	"path/filepath"

	"github.com/pkg/errors"
	"gopkg.in/yaml.v2"

	"gitlab.com/postgres-ai/joe/pkg/pgexplain"
)

const (
	// explainPath declares the directory where explain config files are stored.
	explainPath = "explain"

	// explainFilename declares name of explain configuration file.
	explainFilename = "explain.yaml"
)

// LoadExplainConfig loads and parses an explain configuration.
func LoadExplainConfig() (pgexplain.ExplainConfig, error) {
	var explainConfig pgexplain.ExplainConfig

	if err := readConfig(&explainConfig, path.Join(explainPath, explainFilename)); err != nil {
		return explainConfig, err
	}

	return explainConfig, nil
}

func readConfig(config interface{}, name string) error {
	cfgPath, err := getConfigPath(name)
	if err != nil {
		return errors.Wrap(err, "failed to build config path")
	}

	b, err := os.ReadFile(cfgPath)
	if err != nil {
		return errors.Errorf("Error loading %s config file: %v", name, err)
	}

	if err := yaml.Unmarshal(b, config); err != nil {
		return errors.Errorf("Error parsing %s config: %v", name, err)
	}

	return nil
}

func getConfigPath(name string) (string, error) {
	binDir, err := filepath.Abs(filepath.Dir(os.Args[0]))
	if err != nil {
		return "", err
	}

	dir, err := filepath.Abs(filepath.Dir(binDir))
	if err != nil {
		return "", err
	}

	return path.Join(dir, name), nil
}
