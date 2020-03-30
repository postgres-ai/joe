/*
2019 © Postgres.ai
*/

package config

import (
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/pkg/errors"
	"gopkg.in/yaml.v2"

	"gitlab.com/postgres-ai/joe/pkg/pgexplain"
)

// LoadExplainConfig loads and parses an explain configuration.
func LoadExplainConfig() (pgexplain.ExplainConfig, error) {
	var explainConfig pgexplain.ExplainConfig

	if err := loadConfig(&explainConfig, "explain.yaml"); err != nil {
		return explainConfig, err
	}

	return explainConfig, nil
}

func loadConfig(config interface{}, name string) error {
	b, err := ioutil.ReadFile(getConfigPath(name))
	if err != nil {
		return errors.Errorf("Error loading %s config file: %v", name, err)
	}

	if err := yaml.Unmarshal(b, config); err != nil {
		return errors.Errorf("Error parsing %s config: %v", name, err)
	}

	return nil
}

func getConfigPath(name string) string {
	bindir, _ := filepath.Abs(filepath.Dir(os.Args[0]))
	dir, _ := filepath.Abs(filepath.Dir(bindir))
	path := dir + string(os.PathSeparator) + "config" + string(os.PathSeparator) + name

	return path
}
