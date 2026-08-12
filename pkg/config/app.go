// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

// Package config provides configuration management for the skog application.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v3"
)

// Config is the root application configuration.
//
// It deliberately holds no AWS credentials or endpoints: those live in ~/.aws/config and
// ~/.aws/credentials, which skog reads and never writes. What is stored here is skog's own
// state and preferences, including which profile was last selected.
type Config struct {
	Skog struct {
		// Profile is the name of the selected AWS profile, remembered across restarts.
		Profile string    `yaml:"profile,omitempty"`
		API     APIConfig `yaml:"api,omitempty"`
		// S3 carries no omitempty: an all-default S3Config is the zero struct, and omitting the
		// section on a save would drop an uncapped max_scanned_keys with it.
		S3    S3Config `yaml:"s3"`
		Style string   `yaml:"style,omitempty"`
	} `yaml:"skog"`
}

// APIConfig holds settings controlling AWS API calls.
type APIConfig struct {
	// Timeout bounds a single API call, in seconds.
	Timeout int `yaml:"timeout"`
}

// S3Config holds settings controlling how S3 listings are fetched.
type S3Config struct {
	// MaxRequestedEntries is how many entries one request asks for, and so how many rows an
	// objects page loads at a time. S3 answers at most MaxEntriesPerRequest of them, whatever is
	// asked for, so a larger value is clamped to it.
	MaxRequestedEntries int `yaml:"max_requested_entries,omitempty"`
	// MaxScannedKeys caps how many keys a recursive aggregate walks, since that walk spends one
	// request per batch of keys. Unlimited lifts the cap: the aggregate then runs until it is
	// done or the user cancels it.
	//
	// It carries no omitempty: Unlimited is the zero value, and a save must not drop the key and
	// silently restore the cap.
	MaxScannedKeys int `yaml:"max_scanned_keys"`
}

const (
	// Unlimited is the MaxScannedKeys value that lifts the cap on a recursive aggregate.
	Unlimited = 0
	// MaxEntriesPerRequest is S3's own ceiling on a single ListObjectsV2 response.
	MaxEntriesPerRequest = 1000
)

// GetAPICallTimeout returns the API call timeout duration.
func (c *Config) GetAPICallTimeout() time.Duration {
	return time.Duration(c.Skog.API.Timeout) * time.Second
}

// GetMaxRequestedEntries returns how many entries one request asks for.
func (c *Config) GetMaxRequestedEntries() int {
	return c.Skog.S3.MaxRequestedEntries
}

// GetMaxScannedKeys returns the cap on a recursive aggregate, Unlimited (zero) for no cap.
func (c *Config) GetMaxScannedKeys() int {
	return c.Skog.S3.MaxScannedKeys
}

// SelectedProfile returns the remembered profile name, empty when none was selected yet.
func (c *Config) SelectedProfile() string {
	return c.Skog.Profile
}

// LoadAppConfig loads the application configuration by merging built-in defaults with the
// user config file. User values take precedence.
//
// A missing config file is not an error: skog needs no configuration of its own to run, as
// everything it connects to comes from the AWS shared configuration.
func LoadAppConfig() (*Config, error) {
	configPath, err := GetConfigPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("error reading config file: %w", err)
		}
		data = nil
	}

	return mergeAppConfig(data)
}

// mergeAppConfig merges a raw user config (with environment variables expanded) on top of the
// built-in defaults and validates the result. See mergeYAMLInto for the merge rule.
func mergeAppConfig(userConfig []byte) (*Config, error) {
	defaults, err := loadDefaultAppConfig()
	if err != nil {
		return nil, err
	}

	cfg := &Config{}
	if err := mergeYAMLInto(defaultConfigData, []byte(os.ExpandEnv(string(userConfig))), cfg); err != nil {
		return nil, err
	}

	validate(cfg, defaults)

	return cfg, nil
}

// validate resets any non-positive numeric field to its default and logs a warning, so a
// typo in the config cannot turn into a zero timeout or an empty listing.
func validate(cfg, def *Config) {
	if cfg.Skog.API.Timeout <= 0 {
		log.Warn().Int("value", cfg.Skog.API.Timeout).Int("default", def.Skog.API.Timeout).
			Msg("invalid api.timeout, using default")
		cfg.Skog.API.Timeout = def.Skog.API.Timeout
	}
	if cfg.Skog.S3.MaxRequestedEntries <= 0 {
		log.Warn().Int("value", cfg.Skog.S3.MaxRequestedEntries).
			Int("default", def.Skog.S3.MaxRequestedEntries).
			Msg("invalid s3.max_requested_entries, using default")
		cfg.Skog.S3.MaxRequestedEntries = def.Skog.S3.MaxRequestedEntries
	}
	// Asking for more than S3 will return would make the setting a lie about the page.
	if cfg.Skog.S3.MaxRequestedEntries > MaxEntriesPerRequest {
		log.Warn().Int("value", cfg.Skog.S3.MaxRequestedEntries).
			Int("limit", MaxEntriesPerRequest).
			Msg("s3.max_requested_entries above what S3 returns, using the limit")
		cfg.Skog.S3.MaxRequestedEntries = MaxEntriesPerRequest
	}
	// Unlimited is a deliberate setting, so only values below it are typos.
	if cfg.Skog.S3.MaxScannedKeys < Unlimited {
		log.Warn().Int("value", cfg.Skog.S3.MaxScannedKeys).
			Int("default", def.Skog.S3.MaxScannedKeys).
			Msg("invalid s3.max_scanned_keys, using default")
		cfg.Skog.S3.MaxScannedKeys = def.Skog.S3.MaxScannedKeys
	}
}

func loadDefaultAppConfig() (*Config, error) {
	cfg := &Config{}
	if err := yaml.Unmarshal(defaultConfigData, cfg); err != nil {
		return nil, fmt.Errorf("error unmarshalling default_config.yaml: %w", err)
	}
	return cfg, nil
}

// Save writes the current configuration back to the config file, creating the config
// directory if this is the first write.
func (c *Config) Save() error {
	configPath, err := GetConfigPath()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		log.Error().Err(err).Msg("failed to create config directory")
		return err
	}

	data, err := yaml.Marshal(c)
	if err != nil {
		log.Error().Err(err).Msg("failed to marshal config")
		return err
	}

	if err := os.WriteFile(configPath, data, 0o644); err != nil {
		log.Error().Err(err).Msg("failed to write config")
		return err
	}

	return nil
}
