// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package config

import (
	"os"
	"path/filepath"
	"testing"
)

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
