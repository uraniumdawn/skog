// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package awscfg

import (
	"os"
	"path/filepath"
	"testing"
)

// write puts content in a file under a temp dir and returns its path.
func write(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}

// loadFrom points the loader at the given files, using "" for a file that must not exist.
func loadFrom(t *testing.T, config, credentials string) []*Profile {
	t.Helper()

	absent := filepath.Join(t.TempDir(), "absent")
	if config == "" {
		config = absent
	}
	if credentials == "" {
		credentials = absent
	}

	t.Setenv("AWS_CONFIG_FILE", config)
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", credentials)
	t.Setenv("AWS_ENDPOINT_URL", "")
	t.Setenv("AWS_ENDPOINT_URL_S3", "")

	profiles, err := LoadProfiles()
	if err != nil {
		t.Fatalf("LoadProfiles() error = %v", err)
	}
	return profiles
}

func TestLoadProfilesReadsBothFiles(t *testing.T) {
	config := write(t, "config", `
[default]
region = eu-central-1

# a profile pointing at MinIO
[profile minio-prod]
region = us-east-1
endpoint_url = https://api.storage.example.com

[sso-session corp]
sso_region = eu-central-1
`)
	credentials := write(t, "credentials", `
[minio-prod]
aws_access_key_id = AKIAEXAMPLE
aws_secret_access_key = secret

[keys-only]
aws_access_key_id = AKIAOTHER
aws_secret_access_key = secret
`)

	profiles := loadFrom(t, config, credentials)

	// Config file order first, then the profile only the credentials file declares. The
	// sso-session section is not a profile.
	wantNames := []string{"default", "minio-prod", "keys-only"}
	if len(profiles) != len(wantNames) {
		t.Fatalf("got %d profiles (%v), want %v", len(profiles), names(profiles), wantNames)
	}
	for i, want := range wantNames {
		if profiles[i].Name != want {
			t.Errorf("profile[%d] = %q, want %q", i, profiles[i].Name, want)
		}
	}

	minio := profiles[1]
	if minio.Region != "us-east-1" {
		t.Errorf("region = %q, want %q", minio.Region, "us-east-1")
	}
	if minio.EndpointURL != "https://api.storage.example.com" {
		t.Errorf("endpoint = %q, want %q", minio.EndpointURL, "https://api.storage.example.com")
	}
	if !minio.IsCustomEndpoint() {
		t.Error("IsCustomEndpoint() = false, want true for a profile with endpoint_url")
	}
	if !minio.InConfig || !minio.InCredentials {
		t.Errorf("minio-prod declared in config=%v credentials=%v, want both",
			minio.InConfig, minio.InCredentials)
	}
	if !minio.HasCredentials {
		t.Error("HasCredentials = false, want true for a profile with aws_access_key_id")
	}

	if def := profiles[0]; def.IsCustomEndpoint() || def.HasCredentials {
		t.Errorf("default: endpoint = %q, hasCredentials = %v, want neither",
			def.EndpointURL, def.HasCredentials)
	}
	if got := profiles[0].EffectiveRegion(); got != "eu-central-1" {
		t.Errorf("default region = %q, want %q", got, "eu-central-1")
	}
}

func TestLoadProfilesWithEitherFileMissing(t *testing.T) {
	t.Run("only credentials", func(t *testing.T) {
		credentials := write(t, "credentials", "[dev]\naws_access_key_id = AKIA\n")
		profiles := loadFrom(t, "", credentials)
		if len(profiles) != 1 || profiles[0].Name != "dev" {
			t.Fatalf("got %v, want [dev]", names(profiles))
		}
		if profiles[0].EffectiveRegion() != DefaultRegion {
			t.Errorf("region = %q, want the fallback %q",
				profiles[0].EffectiveRegion(), DefaultRegion)
		}
	})

	t.Run("neither file", func(t *testing.T) {
		if profiles := loadFrom(t, "", ""); len(profiles) != 0 {
			t.Fatalf("got %v, want no profiles", names(profiles))
		}
	})
}

// A profile can point at a [services] section instead of setting endpoint_url directly,
// which is how the AWS SDKs configure a service-specific endpoint.
func TestLoadProfilesResolvesServicesEndpoint(t *testing.T) {
	config := write(t, "config", `
[profile local]
region = us-east-1
services = local-stack

[services local-stack]
s3 =
  endpoint_url = http://localhost:9000
`)

	profiles := loadFrom(t, config, "")
	if len(profiles) != 1 {
		t.Fatalf("got %v, want [local]", names(profiles))
	}
	if got := profiles[0].EndpointURL; got != "http://localhost:9000" {
		t.Errorf("endpoint = %q, want %q", got, "http://localhost:9000")
	}
}

func TestLoadProfilesEnvEndpointFillsInOnly(t *testing.T) {
	config := write(t, "config", `
[profile aws-only]
region = eu-west-1

[profile own-endpoint]
endpoint_url = http://own:9000
`)

	t.Setenv("AWS_CONFIG_FILE", config)
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(t.TempDir(), "absent"))
	t.Setenv("AWS_ENDPOINT_URL_S3", "http://from-env:9000")

	profiles, err := LoadProfiles()
	if err != nil {
		t.Fatalf("LoadProfiles() error = %v", err)
	}
	if got := profiles[0].EndpointURL; got != "http://from-env:9000" {
		t.Errorf("aws-only endpoint = %q, want the environment's %q", got, "http://from-env:9000")
	}
	if got := profiles[1].EndpointURL; got != "http://own:9000" {
		t.Errorf("own-endpoint endpoint = %q, want its own %q", got, "http://own:9000")
	}
}

func TestParseINI(t *testing.T) {
	sections := parseINI(`
; leading comment
[profile dev]
region = us-east-1   # trailing comment
endpoint_url = http://host:9000/path#fragment
malformed line without separator

[services x]
s3 =
  endpoint_url = http://nested:9000
  addressing_style = path
`)

	if len(sections) != 2 {
		t.Fatalf("got %d sections, want 2", len(sections))
	}

	dev := sections[0]
	if dev.name != "profile dev" {
		t.Errorf("section name = %q, want %q", dev.name, "profile dev")
	}
	if got := dev.values["region"]; got != "us-east-1" {
		t.Errorf("region = %q, want %q (trailing comment stripped)", got, "us-east-1")
	}
	// A # without preceding whitespace is part of the value, not a comment.
	if got := dev.values["endpoint_url"]; got != "http://host:9000/path#fragment" {
		t.Errorf("endpoint_url = %q, want the value kept verbatim", got)
	}

	services := sections[1]
	if got := services.values["s3.endpoint_url"]; got != "http://nested:9000" {
		t.Errorf("s3.endpoint_url = %q, want %q", got, "http://nested:9000")
	}
	if got := services.values["s3.addressing_style"]; got != "path" {
		t.Errorf("s3.addressing_style = %q, want %q", got, "path")
	}
}

func names(profiles []*Profile) []string {
	out := make([]string, 0, len(profiles))
	for _, p := range profiles {
		out = append(out, p.Name)
	}
	return out
}
