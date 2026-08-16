// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package config

import _ "embed"

//go:embed default_config.yaml
var defaultConfigData []byte

//go:embed default_style.yaml
var defaultStyleData []byte
