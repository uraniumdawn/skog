// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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

// ResolveUserPath turns a path from the config file into an absolute one.
//
// A leading "~" and a relative path both resolve against the home directory: skog is started
// from whatever directory the user's shell happened to be in, so the working directory says
// nothing about where they meant.
func ResolveUserPath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("empty path")
	}

	if path == "~" || strings.HasPrefix(path, "~"+string(filepath.Separator)) {
		path = strings.TrimPrefix(path[1:], string(filepath.Separator))
	} else if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot resolve %q against the home directory: %w", path, err)
	}
	return filepath.Join(home, path), nil
}

// GetHistoryPath returns the path to the file holding the application's usage history.
func GetHistoryPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "history.yaml"), nil
}
