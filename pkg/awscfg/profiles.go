// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

// Package awscfg reads the AWS shared configuration files (~/.aws/config and
// ~/.aws/credentials) to discover the profiles skog can work with.
//
// The AWS SDK resolves credentials for a named profile itself; what it does not expose is the
// list of profile names, nor the region and endpoint of each. That listing is what this
// package provides, so the UI can show the profiles and hand the chosen name back to the SDK.
package awscfg

import (
	"os"
	"path/filepath"
	"strings"
)

// Profile is one AWS shared-configuration profile as skog needs it: the name to hand to the
// SDK, plus the fields skog displays and acts on.
type Profile struct {
	Name string

	// Region is the profile's region setting, empty when it sets none. The S3 client falls
	// back to DefaultRegion in that case, since S3 requires some region to sign with.
	Region string

	// EndpointURL points at an S3-compatible endpoint (MinIO, LocalStack, Ceph). It comes
	// from endpoint_url in the profile, from the s3 entry of the [services] section the
	// profile references, or from AWS_ENDPOINT_URL_S3/AWS_ENDPOINT_URL. Empty means AWS.
	EndpointURL string

	// InConfig and InCredentials record which files declared the profile. A profile present
	// only in ~/.aws/config may still work (SSO, IAM role, instance metadata), so neither
	// flag is a precondition for using it — they are shown to explain where it came from.
	InConfig       bool
	InCredentials  bool
	HasCredentials bool // profile carries aws_access_key_id in ~/.aws/credentials or ~/.aws/config
}

// DefaultRegion is used for profiles that set no region. S3 requires a region to sign
// requests, and S3-compatible endpoints generally ignore which one it is.
const DefaultRegion = "us-east-1"

// IsCustomEndpoint reports whether the profile points at an S3-compatible endpoint rather
// than AWS itself.
func (p *Profile) IsCustomEndpoint() bool {
	return p.EndpointURL != ""
}

// EffectiveRegion returns the profile's region, or DefaultRegion when it sets none.
func (p *Profile) EffectiveRegion() string {
	if p.Region == "" {
		return DefaultRegion
	}
	return p.Region
}

// ConfigPath returns the path of the shared config file: $AWS_CONFIG_FILE or ~/.aws/config.
func ConfigPath() string {
	if path := os.Getenv("AWS_CONFIG_FILE"); path != "" {
		return path
	}
	return filepath.Join(homeDir(), ".aws", "config")
}

// CredentialsPath returns the path of the shared credentials file:
// $AWS_SHARED_CREDENTIALS_FILE or ~/.aws/credentials.
func CredentialsPath() string {
	if path := os.Getenv("AWS_SHARED_CREDENTIALS_FILE"); path != "" {
		return path
	}
	return filepath.Join(homeDir(), ".aws", "credentials")
}

func homeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

// LoadProfiles reads the shared config and credentials files and returns the profiles they
// declare, config file order first, then profiles only the credentials file knows about.
//
// A missing file is not an error: either file alone is a valid setup. An unreadable one is
// reported, since silently showing fewer profiles would be worse than saying why.
func LoadProfiles() ([]*Profile, error) {
	config, configErr := readINI(ConfigPath())
	credentials, credentialsErr := readINI(CredentialsPath())
	if configErr != nil {
		return nil, configErr
	}
	if credentialsErr != nil {
		return nil, credentialsErr
	}

	return buildProfiles(config, credentials), nil
}

// buildProfiles merges the parsed sections of both files into the profile list.
func buildProfiles(config, credentials []section) []*Profile {
	// [services <name>] sections hold per-service overrides, including the S3 endpoint of a
	// profile that says `services = <name>`.
	services := make(map[string]section)
	for _, s := range config {
		if name, ok := sectionName(s.name, "services"); ok {
			services[name] = s
		}
	}

	var profiles []*Profile
	index := make(map[string]*Profile)

	add := func(name string) *Profile {
		if p, ok := index[name]; ok {
			return p
		}
		p := &Profile{Name: name}
		index[name] = p
		profiles = append(profiles, p)
		return p
	}

	for _, s := range config {
		name, ok := profileSectionName(s.name)
		if !ok {
			continue
		}
		p := add(name)
		p.InConfig = true
		p.Region = s.values["region"]
		p.EndpointURL = endpointOf(s, services)
		p.HasCredentials = s.values["aws_access_key_id"] != ""
	}

	// The credentials file uses bare section names and normally carries only keys; anything
	// the config file already stated wins, since that is the file those settings belong in.
	for _, s := range credentials {
		if s.name == "" {
			continue
		}
		p := add(s.name)
		p.InCredentials = true
		if s.values["aws_access_key_id"] != "" {
			p.HasCredentials = true
		}
		if p.Region == "" {
			p.Region = s.values["region"]
		}
		if p.EndpointURL == "" {
			p.EndpointURL = s.values["endpoint_url"]
		}
	}

	if fallback := endpointFromEnv(); fallback != "" {
		for _, p := range profiles {
			if p.EndpointURL == "" {
				p.EndpointURL = fallback
			}
		}
	}

	return profiles
}

// endpointOf resolves a profile's S3 endpoint: its own endpoint_url first, then the s3
// endpoint_url of the [services] section it references.
func endpointOf(profile section, services map[string]section) string {
	if endpoint := profile.values["endpoint_url"]; endpoint != "" {
		return endpoint
	}
	if name := profile.values["services"]; name != "" {
		if s, ok := services[name]; ok {
			return s.values["s3.endpoint_url"]
		}
	}
	return ""
}

// endpointFromEnv returns the endpoint the environment sets for S3, if any. The
// service-specific variable wins over the global one, as it does in the AWS SDKs.
func endpointFromEnv() string {
	if endpoint := os.Getenv("AWS_ENDPOINT_URL_S3"); endpoint != "" {
		return endpoint
	}
	return os.Getenv("AWS_ENDPOINT_URL")
}

// profileSectionName maps a section header to a profile name: "[default]" is the default
// profile, "[profile x]" is profile x. Any other prefixed section (sso-session, services)
// is not a profile.
func profileSectionName(header string) (string, bool) {
	if header == "default" {
		return "default", true
	}
	return sectionName(header, "profile")
}

// sectionName splits a two-word section header such as "profile dev" into its kind and name.
func sectionName(header, kind string) (string, bool) {
	prefix := kind + " "
	if !strings.HasPrefix(header, prefix) {
		return "", false
	}
	name := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	if name == "" {
		return "", false
	}
	return name, true
}
