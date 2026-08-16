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
	"strings"
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
		S3       S3Config       `yaml:"s3"`
		Download DownloadConfig `yaml:"download"`
		Viewer   ViewerConfig   `yaml:"viewer"`
		// Cache carries no omitempty for the same reason S3 does not: Unlimited is the zero
		// value of its budget, and omitting the section would drop an uncapped cache with it.
		Cache CacheConfig `yaml:"cache"`
		Style string      `yaml:"style,omitempty"`
		// Modes is the mode of each profile that is not in the default one, keyed by profile
		// name. See Mode; <Tab> on the Profiles page is what writes here.
		Modes map[string]string `yaml:"modes,omitempty"`
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

// DownloadConfig holds where <d> writes what it fetches.
type DownloadConfig struct {
	// Dir is the folder downloads are written into, as <dir>/<bucket>/<key>. Environment
	// variables are expanded, and "~" and a relative path both resolve against the home
	// directory: skog is started from whatever directory the shell happened to be in.
	Dir string `yaml:"dir"`
}

// ViewerConfig holds what the viewer will read and how much of it is shown at a time.
type ViewerConfig struct {
	// MaxObjectSizeMB is the largest object the viewer will fetch. The viewer parses a whole file, so
	// an object over this is refused rather than started on: the message names the size, and
	// <d> is what fetches something that big.
	MaxObjectSizeMB int `yaml:"max_object_size_mb,omitempty"`
	// PageRows is how many rows one batch holds, and so how many <n> adds.
	PageRows int `yaml:"page_rows,omitempty"`
}

// CacheConfig holds where object bodies are kept between views, and how much of them.
type CacheConfig struct {
	// Dir is the folder cached bodies are written into. Environment variables are expanded,
	// and "~" and a relative path both resolve against the home directory, as download.dir
	// does.
	Dir string `yaml:"dir"`
	// MaxSizeMB caps what the cache holds on disk, the least recently viewed object going
	// first once it is over. Unlimited lifts the cap.
	//
	// It carries no omitempty: Unlimited is the zero value, and a save must not drop the key
	// and silently restore the cap.
	MaxSizeMB int `yaml:"max_size_mb"`
}

const (
	// Unlimited is the value that lifts a cap: on the keys a recursive aggregate walks, and on
	// what the viewer's cache holds.
	Unlimited = 0
	// MaxEntriesPerRequest is S3's own ceiling on a single ListObjectsV2 response.
	MaxEntriesPerRequest = 1000
	// bytesPerMB converts the megabytes the config is written in to the bytes the code counts.
	bytesPerMB = 1 << 20
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

// DownloadDir returns the folder downloads are written into, as an absolute path.
func (c *Config) DownloadDir() (string, error) {
	return ResolveUserPath(c.Skog.Download.Dir)
}

// MaxViewableObjectSize returns the largest object the viewer will fetch, in bytes.
func (c *Config) MaxViewableObjectSize() int64 {
	return int64(c.Skog.Viewer.MaxObjectSizeMB) * bytesPerMB
}

// ViewerPageRows returns how many rows one batch of the viewer holds.
func (c *Config) ViewerPageRows() int {
	return c.Skog.Viewer.PageRows
}

// CacheDir returns the folder cached object bodies are kept in, as an absolute path.
func (c *Config) CacheDir() (string, error) {
	return ResolveUserPath(c.Skog.Cache.Dir)
}

// CacheMaxSize returns the cache's budget in bytes, zero for no cap.
func (c *Config) CacheMaxSize() int64 {
	return int64(c.Skog.Cache.MaxSizeMB) * bytesPerMB
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
	// A misspelled mode is dropped rather than kept: a profile skog cannot make sense of the
	// mode of is worked with in the default one, and the file should say so.
	for profile, mode := range cfg.Skog.Modes {
		if !Mode(mode).valid() {
			log.Warn().Str("profile", profile).Str("value", mode).
				Str("default", string(Regular)).Msg("invalid mode, using default")
			delete(cfg.Skog.Modes, profile)
		}
	}
	// An empty download folder is no folder at all, so <d> would have nowhere to write.
	if strings.TrimSpace(cfg.Skog.Download.Dir) == "" {
		log.Warn().Str("default", def.Skog.Download.Dir).
			Msg("empty download.dir, using default")
		cfg.Skog.Download.Dir = def.Skog.Download.Dir
	}
	if cfg.Skog.Viewer.MaxObjectSizeMB <= 0 {
		log.Warn().Int("value", cfg.Skog.Viewer.MaxObjectSizeMB).
			Int("default", def.Skog.Viewer.MaxObjectSizeMB).
			Msg("invalid viewer.max_object_size_mb, using default")
		cfg.Skog.Viewer.MaxObjectSizeMB = def.Skog.Viewer.MaxObjectSizeMB
	}
	if cfg.Skog.Viewer.PageRows <= 0 {
		log.Warn().Int("value", cfg.Skog.Viewer.PageRows).
			Int("default", def.Skog.Viewer.PageRows).
			Msg("invalid viewer.page_rows, using default")
		cfg.Skog.Viewer.PageRows = def.Skog.Viewer.PageRows
	}
	// An empty cache folder is no folder at all, so the viewer would have nowhere to put a body.
	if strings.TrimSpace(cfg.Skog.Cache.Dir) == "" {
		log.Warn().Str("default", def.Skog.Cache.Dir).Msg("empty cache.dir, using default")
		cfg.Skog.Cache.Dir = def.Skog.Cache.Dir
	}
	// Unlimited is a deliberate setting, so only values below it are typos.
	if cfg.Skog.Cache.MaxSizeMB < Unlimited {
		log.Warn().Int("value", cfg.Skog.Cache.MaxSizeMB).
			Int("default", def.Skog.Cache.MaxSizeMB).
			Msg("invalid cache.max_size_mb, using default")
		cfg.Skog.Cache.MaxSizeMB = def.Skog.Cache.MaxSizeMB
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
