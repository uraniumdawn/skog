// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package config

// Mode is how much skog may change what a profile points at. It is a property of the profile,
// not of the session: the mode a bucket is worked with follows the profile it belongs to.
type Mode string

const (
	// ReadOnly refuses every modifying action.
	ReadOnly Mode = "read-only"
	// Regular allows a modifying action, each one confirmed first.
	Regular Mode = "regular"
	// Yolo allows a modifying action with no confirmation.
	Yolo Mode = "yolo"
)

// modeCycle is the order <Tab> walks the modes in, starting from the safest.
var modeCycle = []Mode{ReadOnly, Regular, Yolo}

// NextMode returns the mode after the given one, wrapping around. An unknown mode is treated as
// Regular, so <Tab> always leads somewhere valid.
func NextMode(mode Mode) Mode {
	for i, m := range modeCycle {
		if m == mode {
			return modeCycle[(i+1)%len(modeCycle)]
		}
	}
	return NextMode(Regular)
}

// ProfileMode returns the mode the named profile is worked with. A profile that was never
// switched is Regular, which is why only what differs from it is stored.
func (c *Config) ProfileMode(profile string) Mode {
	mode := Mode(c.Skog.Modes[profile])
	if !mode.valid() {
		return Regular
	}
	return mode
}

// SetProfileMode records the mode the named profile is worked with. Regular is the default, so
// it is stored as the absence of an entry rather than as a value.
func (c *Config) SetProfileMode(profile string, mode Mode) {
	if !mode.valid() || mode == Regular {
		delete(c.Skog.Modes, profile)
		return
	}
	if c.Skog.Modes == nil {
		c.Skog.Modes = make(map[string]string, 1)
	}
	c.Skog.Modes[profile] = string(mode)
}

// valid reports whether the mode is one skog knows.
func (m Mode) valid() bool {
	switch m {
	case ReadOnly, Regular, Yolo:
		return true
	default:
		return false
	}
}
