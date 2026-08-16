// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package config

import (
	"os"
	"path/filepath"
	"testing"
)

// The merge rule: a key the user set wins whatever its value, a key the user left out keeps
// its default.
func TestMergeYAMLInto(t *testing.T) {
	const defaults = `
root:
  text: default-text
  number: 30
  flag: true
  list:
    - a
    - b
  nested:
    kept: default-kept
    replaced: default-replaced
`

	type nested struct {
		Kept     string `yaml:"kept"`
		Replaced string `yaml:"replaced"`
	}
	type root struct {
		Root struct {
			Text   string   `yaml:"text"`
			Number int      `yaml:"number"`
			Flag   *bool    `yaml:"flag"`
			List   []string `yaml:"list"`
			Nested nested   `yaml:"nested"`
			Extra  string   `yaml:"extra"`
		} `yaml:"root"`
	}

	tests := []struct {
		name  string
		user  string
		check func(t *testing.T, got root)
	}{
		{
			name: "empty user document keeps every default",
			user: "",
			check: func(t *testing.T, got root) {
				if got.Root.Text != "default-text" || got.Root.Number != 30 {
					t.Errorf("defaults lost: %+v", got.Root)
				}
				if got.Root.Flag == nil || !*got.Root.Flag {
					t.Error("flag default should stay true")
				}
			},
		},
		{
			name: "false overrides a true default",
			user: "root:\n  flag: false\n",
			check: func(t *testing.T, got root) {
				if got.Root.Flag == nil || *got.Root.Flag {
					t.Errorf("flag = %v, want false", got.Root.Flag)
				}
				if got.Root.Text != "default-text" {
					t.Errorf("unrelated default lost: %q", got.Root.Text)
				}
			},
		},
		{
			name: "zero and empty string override",
			user: "root:\n  number: 0\n  text: \"\"\n",
			check: func(t *testing.T, got root) {
				if got.Root.Number != 0 {
					t.Errorf("number = %d, want 0", got.Root.Number)
				}
				if got.Root.Text != "" {
					t.Errorf("text = %q, want empty", got.Root.Text)
				}
			},
		},
		{
			name: "empty list overrides the default list",
			user: "root:\n  list: []\n",
			check: func(t *testing.T, got root) {
				if len(got.Root.List) != 0 {
					t.Errorf("list = %v, want empty", got.Root.List)
				}
			},
		},
		{
			name: "list is replaced wholesale, not appended",
			user: "root:\n  list:\n    - c\n",
			check: func(t *testing.T, got root) {
				if len(got.Root.List) != 1 || got.Root.List[0] != "c" {
					t.Errorf("list = %v, want [c]", got.Root.List)
				}
			},
		},
		{
			name: "explicit null clears a value",
			user: "root:\n  text: ~\n",
			check: func(t *testing.T, got root) {
				if got.Root.Text != "" {
					t.Errorf("text = %q, want empty", got.Root.Text)
				}
			},
		},
		{
			name: "nested override keeps sibling defaults",
			user: "root:\n  nested:\n    replaced: user-replaced\n",
			check: func(t *testing.T, got root) {
				if got.Root.Nested.Replaced != "user-replaced" {
					t.Errorf("replaced = %q", got.Root.Nested.Replaced)
				}
				if got.Root.Nested.Kept != "default-kept" {
					t.Errorf("kept = %q, want the default", got.Root.Nested.Kept)
				}
			},
		},
		{
			name: "keys absent from the defaults are added",
			user: "root:\n  extra: user-extra\n",
			check: func(t *testing.T, got root) {
				if got.Root.Extra != "user-extra" {
					t.Errorf("extra = %q", got.Root.Extra)
				}
				if got.Root.Text != "default-text" {
					t.Errorf("unrelated default lost: %q", got.Root.Text)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got root
			if err := mergeYAMLInto([]byte(defaults), []byte(tt.user), &got); err != nil {
				t.Fatalf("mergeYAMLInto() error = %v", err)
			}
			tt.check(t, got)
		})
	}
}

func TestMergeYAMLIntoInvalidUserDocument(t *testing.T) {
	var got struct{}
	if err := mergeYAMLInto([]byte("a: 1\n"), []byte("\tnot yaml"), &got); err == nil {
		t.Error("expected an error for a malformed user document")
	}
}

// The app config follows the same rule: every key the user sets wins, every key they leave
// out keeps its built-in default, including the siblings of an overridden key.
func TestAppConfigOverrides(t *testing.T) {
	defaults, err := loadDefaultAppConfig()
	if err != nil {
		t.Fatalf("loadDefaultAppConfig() error = %v", err)
	}

	tests := []struct {
		name  string
		user  string
		check func(*testing.T, *Config)
	}{
		{
			name: "no user config at all keeps every default",
			user: "",
			check: func(t *testing.T, cfg *Config) {
				if cfg.Skog.API.Timeout != defaults.Skog.API.Timeout {
					t.Errorf("api.timeout = %d, want the default %d",
						cfg.Skog.API.Timeout, defaults.Skog.API.Timeout)
				}
				if cfg.Skog.S3.MaxRequestedEntries != defaults.Skog.S3.MaxRequestedEntries {
					t.Errorf("s3.max_requested_entries = %d, want the default %d",
						cfg.Skog.S3.MaxRequestedEntries, defaults.Skog.S3.MaxRequestedEntries)
				}
			},
		},
		{
			name: "an overridden key wins and its siblings survive",
			user: "skog:\n  s3:\n    max_requested_entries: 50\n",
			check: func(t *testing.T, cfg *Config) {
				if cfg.Skog.S3.MaxRequestedEntries != 50 {
					t.Errorf(
						"s3.max_requested_entries = %d, want 50",
						cfg.Skog.S3.MaxRequestedEntries,
					)
				}
				if cfg.Skog.S3.MaxScannedKeys != defaults.Skog.S3.MaxScannedKeys {
					t.Errorf("s3.max_scanned_keys = %d, want the default %d",
						cfg.Skog.S3.MaxScannedKeys, defaults.Skog.S3.MaxScannedKeys)
				}
				if cfg.Skog.API.Timeout != defaults.Skog.API.Timeout {
					t.Errorf("api.timeout = %d, want the default %d",
						cfg.Skog.API.Timeout, defaults.Skog.API.Timeout)
				}
			},
		},
		{
			name: "a value that cannot work falls back to the default",
			user: "skog:\n  api:\n    timeout: 0\n  s3:\n    max_requested_entries: -1\n",
			check: func(t *testing.T, cfg *Config) {
				if cfg.Skog.API.Timeout != defaults.Skog.API.Timeout {
					t.Errorf("api.timeout = %d, want the default %d",
						cfg.Skog.API.Timeout, defaults.Skog.API.Timeout)
				}
				if cfg.Skog.S3.MaxRequestedEntries != defaults.Skog.S3.MaxRequestedEntries {
					t.Errorf("s3.max_requested_entries = %d, want the default %d",
						cfg.Skog.S3.MaxRequestedEntries, defaults.Skog.S3.MaxRequestedEntries)
				}
			},
		},
		{
			// 0 is how the user lifts the scan cap, so validation must let it through instead of
			// reading it as an unset field.
			name: "an unlimited scan cap is kept",
			user: "skog:\n  s3:\n    max_scanned_keys: 0\n",
			check: func(t *testing.T, cfg *Config) {
				if got := cfg.GetMaxScannedKeys(); got != Unlimited {
					t.Errorf("s3.max_scanned_keys = %d, want %d", got, Unlimited)
				}
			},
		},
		{
			name: "a scan cap that cannot work falls back to the default",
			user: "skog:\n  s3:\n    max_scanned_keys: -1\n",
			check: func(t *testing.T, cfg *Config) {
				if got := cfg.GetMaxScannedKeys(); got != defaults.Skog.S3.MaxScannedKeys {
					t.Errorf("s3.max_scanned_keys = %d, want the default %d",
						got, defaults.Skog.S3.MaxScannedKeys)
				}
			},
		},
		{
			// S3 never returns more than MaxEntriesPerRequest, so asking for more is pointless.
			name: "a request size above what S3 returns is clamped",
			user: "skog:\n  s3:\n    max_requested_entries: 5000\n",
			check: func(t *testing.T, cfg *Config) {
				if got := cfg.GetMaxRequestedEntries(); got != MaxEntriesPerRequest {
					t.Errorf("s3.max_requested_entries = %d, want %d", got, MaxEntriesPerRequest)
				}
			},
		},
		{
			name: "the selected profile is read back",
			user: "skog:\n  profile: minio-prod\n",
			check: func(t *testing.T, cfg *Config) {
				if got := cfg.SelectedProfile(); got != "minio-prod" {
					t.Errorf("profile = %q, want %q", got, "minio-prod")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := mergeAppConfig([]byte(tt.user))
			if err != nil {
				t.Fatalf("mergeAppConfig() error = %v", err)
			}
			tt.check(t, cfg)
		})
	}
}

// The style config uses the same merge, so a partial style file only changes what it names.
func TestLoadColorConfigMergesOnTopOfDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "style.yaml")
	style := "skog:\n  label:\n    fgColor: \"red\"\n  title: \"blue\"\n"
	if err := os.WriteFile(path, []byte(style), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	colors, err := LoadColorConfig(path)
	if err != nil {
		t.Fatalf("LoadColorConfig() error = %v", err)
	}

	if got := colors.Skog.Label.FgColor; got != "red" {
		t.Errorf("label.fgColor = %q, want %q", got, "red")
	}
	if got := colors.Skog.Title; got != "blue" {
		t.Errorf("title = %q, want %q", got, "blue")
	}
	// Sibling of an overridden key, and a key the style file never mentions.
	if got := colors.Skog.Label.BgColor; got != "default" {
		t.Errorf("label.bgColor = %q, want the default %q", got, "default")
	}
	if got := colors.Skog.Selection.BgColor; got != "white" {
		t.Errorf("selection.bgColor = %q, want the default %q", got, "white")
	}
}

// Every example theme is a complete file, and each still has to load.
func TestExampleStylesLoad(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "examples", "style", "*.yaml"))
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("no example style files found")
	}

	for _, path := range paths {
		if _, err := LoadColorConfig(path); err != nil {
			t.Errorf("LoadColorConfig(%s) error = %v", path, err)
		}
	}
}

func TestLoadColorConfigDefaultsWhenNoStylePath(t *testing.T) {
	colors, err := LoadColorConfig("")
	if err != nil {
		t.Fatalf("LoadColorConfig() error = %v", err)
	}
	if got := colors.Skog.Label.FgColor; got != "orange" {
		t.Errorf("label.fgColor = %q, want the default %q", got, "orange")
	}
}

func TestLoadColorConfigMissingFile(t *testing.T) {
	if _, err := LoadColorConfig(filepath.Join(t.TempDir(), "absent.yaml")); err == nil {
		t.Error("expected an error for a missing style file")
	}
}
