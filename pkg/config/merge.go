// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package config

import (
	"fmt"
	"maps"

	"gopkg.in/yaml.v3"
)

// mergeYAMLInto merges the user's document on top of the built-in defaults document and
// decodes the result into out. It is the single merge rule behind both the app config and the
// style config.
//
// Merging is by presence, not by value: every key the user set wins — including false, 0, an
// empty string, an empty list, or an explicit null — and every key the user left out keeps its
// default. Nested mappings merge key by key, so overriding one field does not drop its
// siblings. Scalars and sequences are replaced wholesale: a list in the user's file overrides
// the default list instead of appending to it.
func mergeYAMLInto(defaults, user []byte, out any) error {
	var defaultTree, userTree any
	if err := yaml.Unmarshal(defaults, &defaultTree); err != nil {
		return fmt.Errorf("error unmarshalling defaults: %w", err)
	}
	if err := yaml.Unmarshal(user, &userTree); err != nil {
		return fmt.Errorf("error unmarshalling config: %w", err)
	}

	// An empty user document states nothing, which must not be read as "set everything to
	// null" the way an explicit null for a single key is.
	merged := defaultTree
	if userTree != nil {
		merged = mergeYAMLValue(defaultTree, userTree)
	}

	data, err := yaml.Marshal(merged)
	if err != nil {
		return fmt.Errorf("error marshalling merged config: %w", err)
	}
	if err := yaml.Unmarshal(data, out); err != nil {
		return fmt.Errorf("error unmarshalling merged config: %w", err)
	}

	return nil
}

// mergeYAMLValue returns the user's value, except when both sides are mappings: those merge
// key by key, so keys only the defaults define survive.
func mergeYAMLValue(def, user any) any {
	defMap, defIsMap := def.(map[string]any)
	userMap, userIsMap := user.(map[string]any)
	if !defIsMap || !userIsMap {
		return user
	}

	merged := make(map[string]any, len(defMap)+len(userMap))
	maps.Copy(merged, defMap)
	for key, userValue := range userMap {
		if defValue, ok := merged[key]; ok {
			merged[key] = mergeYAMLValue(defValue, userValue)
		} else {
			merged[key] = userValue
		}
	}

	return merged
}
