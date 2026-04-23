package config

import (
	"os"
)

// ExpandFileToTemp writes an env-expanded copy of a config file to a temporary path.
func ExpandFileToTemp(path string) (string, func(), error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", nil, err
	}

	tmpFile, err := os.CreateTemp("", "joe-config-*.yml")
	if err != nil {
		return "", nil, err
	}

	cleanup := func() {
		_ = os.Remove(tmpFile.Name())
	}

	if _, err := tmpFile.Write([]byte(os.ExpandEnv(string(data)))); err != nil {
		cleanup()
		_ = tmpFile.Close()
		return "", nil, err
	}

	if err := tmpFile.Close(); err != nil {
		cleanup()
		return "", nil, err
	}

	return tmpFile.Name(), cleanup, nil
}
