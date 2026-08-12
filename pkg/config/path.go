// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package config

import (
	"os"
	"path/filepath"

	"github.com/rs/zerolog/log"
)

const (
	// SkogEnvConfigDir is the environment variable name for custom config directory.
	SkogEnvConfigDir = "SKOG_CONFIG_DIR"
)

func isEnvSet(env string) bool {
	return os.Getenv(env) != ""
}

// configDir returns the directory holding the application's files: <SKOG_CONFIG_DIR or
// $HOME>/.config/skog.
func configDir() (string, error) {
	var base string
	switch {
	case isEnvSet(SkogEnvConfigDir):
		base = os.Getenv(SkogEnvConfigDir)
	default:
		homeDir, err := os.UserHomeDir()
		if err != nil {
			log.Fatal().Err(err).Msg("error getting home directory")
			return "", err
		}
		base = homeDir
	}
	return filepath.Join(base, ".config", "skog"), nil
}

// GetConfigPath returns the path to the application configuration file.
func GetConfigPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.yaml"), nil
}

// GetHistoryPath returns the path to the file holding the application's usage history.
func GetHistoryPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "history.yaml"), nil
}
