// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package awscfg

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

// section is one [header] block of a shared configuration file with its key/value pairs.
// Nested blocks are flattened into dotted keys, so the s3 endpoint of
//
//	[services local]
//	s3 =
//	  endpoint_url = http://localhost:9000
//
// is stored under "s3.endpoint_url".
type section struct {
	name   string
	values map[string]string
}

// readINI parses the file at path. A file that does not exist yields no sections and no
// error: neither ~/.aws/config nor ~/.aws/credentials is required to be present.
func readINI(path string) ([]section, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return parseINI(string(data)), nil
}

// parseINI parses the AWS flavour of INI: [section] headers, key = value pairs, whole-line
// and trailing comments starting with # or ;, and nested blocks introduced by a key with an
// empty value and continued by indented keys.
//
// Keys are lower-cased, values are kept verbatim. Malformed lines are skipped rather than
// rejected: a file skog cannot fully understand should still list the profiles it can.
func parseINI(text string) []section {
	var sections []section
	current := -1
	parentKey := ""

	for raw := range strings.SplitSeq(text, "\n") {
		line := strings.TrimRight(raw, " \t\r")
		trimmed := strings.TrimSpace(line)

		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") {
			continue
		}

		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			name := strings.TrimSpace(trimmed[1 : len(trimmed)-1])
			sections = append(sections, section{name: name, values: map[string]string{}})
			current = len(sections) - 1
			parentKey = ""
			continue
		}

		if current < 0 {
			continue
		}

		key, value, ok := splitKeyValue(trimmed)
		if !ok {
			continue
		}

		// An indented key belongs to the nested block opened by the last valueless key.
		if line != trimmed && parentKey != "" {
			sections[current].values[parentKey+"."+key] = value
			continue
		}

		if value == "" {
			parentKey = key
		} else {
			parentKey = ""
		}
		sections[current].values[key] = value
	}

	return sections
}

// splitKeyValue splits "key = value" and strips a trailing comment from the value. It
// reports false for a line with no separator.
func splitKeyValue(line string) (key, value string, ok bool) {
	name, rest, found := strings.Cut(line, "=")
	if !found {
		return "", "", false
	}
	key = strings.ToLower(strings.TrimSpace(name))
	if key == "" {
		return "", "", false
	}
	return key, stripComment(strings.TrimSpace(rest)), true
}

// stripComment removes a trailing comment. As in the AWS parsers, a comment marker only
// starts a comment when whitespace precedes it, so it may appear inside a value.
func stripComment(value string) string {
	for i := 1; i < len(value); i++ {
		if value[i] != '#' && value[i] != ';' {
			continue
		}
		if prev := value[i-1]; prev == ' ' || prev == '\t' {
			return strings.TrimSpace(value[:i])
		}
	}
	return value
}
