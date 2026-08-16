// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package config

import (
	"os"
	"path/filepath"
	"testing"
)

// A mode is switched with <Tab> and saved on the spot, so it has to survive the round trip
// through the config file — a profile pinned to read-only must still be read-only next start.
func TestSaveKeepsProfileModes(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(SkogEnvConfigDir, dir)

	path := filepath.Join(dir, ".config", "skog", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	user := "skog:\n  profile: minio-prod\n  modes:\n    minio-prod: read-only\n"
	if err := os.WriteFile(path, []byte(user), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := LoadAppConfig()
	if err != nil {
		t.Fatalf("LoadAppConfig() error = %v", err)
	}
	if got := cfg.ProfileMode("minio-prod"); got != ReadOnly {
		t.Fatalf("mode = %q, want %q", got, ReadOnly)
	}

	cfg.SetProfileMode("local", Yolo)
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	reloaded, err := LoadAppConfig()
	if err != nil {
		t.Fatalf("LoadAppConfig() after Save() error = %v", err)
	}
	if got := reloaded.ProfileMode("minio-prod"); got != ReadOnly {
		t.Errorf("mode after Save() = %q, want %q", got, ReadOnly)
	}
	if got := reloaded.ProfileMode("local"); got != Yolo {
		t.Errorf("mode after Save() = %q, want %q", got, Yolo)
	}
}

// A profile nothing was said about is worked with in the mode that asks first.
func TestProfileModeDefaultsToRegular(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(SkogEnvConfigDir, dir)

	cfg, err := LoadAppConfig()
	if err != nil {
		t.Fatalf("LoadAppConfig() error = %v", err)
	}
	if got := cfg.ProfileMode("minio-prod"); got != Regular {
		t.Errorf("mode with no config file = %q, want %q", got, Regular)
	}
}

// A mode skog cannot make sense of must not leave a profile in an undefined state, nor stay in
// the file to be misread next start.
func TestInvalidProfileModeIsDropped(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(SkogEnvConfigDir, dir)

	path := filepath.Join(dir, ".config", "skog", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	user := "skog:\n  modes:\n    minio-prod: read_only\n    local: yolo\n"
	if err := os.WriteFile(path, []byte(user), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := LoadAppConfig()
	if err != nil {
		t.Fatalf("LoadAppConfig() error = %v", err)
	}
	if got := cfg.ProfileMode("minio-prod"); got != Regular {
		t.Errorf("mode of a misspelled entry = %q, want %q", got, Regular)
	}
	if _, ok := cfg.Skog.Modes["minio-prod"]; ok {
		t.Errorf("misspelled entry kept in the config, want it dropped")
	}
	if got := cfg.ProfileMode("local"); got != Yolo {
		t.Errorf("mode of the valid entry = %q, want %q", got, Yolo)
	}
}

// An uncapped scan is the zero value of its field, so saving the config — which skog does
// whenever a profile is selected — must not drop the key and quietly restore the cap.
func TestSaveKeepsAnUnlimitedScanCap(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(SkogEnvConfigDir, dir)

	path := filepath.Join(dir, ".config", "skog", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	user := "skog:\n  profile: minio-prod\n  s3:\n    max_scanned_keys: 0\n"
	if err := os.WriteFile(path, []byte(user), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := LoadAppConfig()
	if err != nil {
		t.Fatalf("LoadAppConfig() error = %v", err)
	}
	if got := cfg.GetMaxScannedKeys(); got != Unlimited {
		t.Fatalf("s3.max_scanned_keys = %d, want %d", got, Unlimited)
	}

	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	reloaded, err := LoadAppConfig()
	if err != nil {
		t.Fatalf("LoadAppConfig() after Save() error = %v", err)
	}
	if got := reloaded.GetMaxScannedKeys(); got != Unlimited {
		t.Errorf("s3.max_scanned_keys after Save() = %d, want %d", got, Unlimited)
	}
}

// <d> writes into the folder the config file names, so what the user typed there has to reach
// the filesystem as a path it understands: "~", an environment variable and a bare name are all
// things they may reasonably write.
func TestDownloadDir(t *testing.T) {
	tests := []struct {
		name string
		user string
		want func(home string) string
	}{
		{
			name: "no config file at all falls back to the built-in folder",
			want: func(home string) string { return filepath.Join(home, "Downloads", "skog") },
		},
		{
			name: "a leading ~ resolves against the home directory",
			user: "skog:\n  download:\n    dir: \"~/objects\"\n",
			want: func(home string) string { return filepath.Join(home, "objects") },
		},
		{
			// The working directory of a TUI is whichever one the shell happened to be in.
			name: "a relative folder resolves against the home directory too",
			user: "skog:\n  download:\n    dir: objects/s3\n",
			want: func(home string) string { return filepath.Join(home, "objects", "s3") },
		},
		{
			name: "an environment variable is expanded",
			user: "skog:\n  download:\n    dir: $SKOG_TEST_HOME/objects\n",
			want: func(home string) string { return filepath.Join(home, "objects") },
		},
		{
			// A folder that is no folder would leave <d> with nowhere to write.
			name: "an empty folder falls back to the built-in one",
			user: "skog:\n  download:\n    dir: \"\"\n",
			want: func(home string) string { return filepath.Join(home, "Downloads", "skog") },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("SKOG_TEST_HOME", home)
			t.Setenv(SkogEnvConfigDir, home)

			if tt.user != "" {
				path := filepath.Join(home, ".config", "skog", "config.yaml")
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatalf("MkdirAll() error = %v", err)
				}
				if err := os.WriteFile(path, []byte(tt.user), 0o644); err != nil {
					t.Fatalf("WriteFile() error = %v", err)
				}
			}

			cfg, err := LoadAppConfig()
			if err != nil {
				t.Fatalf("LoadAppConfig() error = %v", err)
			}

			got, err := cfg.DownloadDir()
			if err != nil {
				t.Fatalf("DownloadDir() error = %v", err)
			}
			if want := tt.want(home); got != want {
				t.Errorf("DownloadDir() = %q, want %q", got, want)
			}
		})
	}
}
