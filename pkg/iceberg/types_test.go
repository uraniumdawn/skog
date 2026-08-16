// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package iceberg

import (
	"encoding/json"
	"testing"
	"time"
)

func TestTypeString(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"primitive", `"long"`, "long"},
		{"decimal keeps its precision", `"decimal(12,2)"`, "decimal(12,2)"},
		{
			name: "list",
			raw:  `{"type":"list","element-id":4,"element":"string","element-required":true}`,
			want: "list<string>",
		},
		{
			name: "map",
			raw:  `{"type":"map","key-id":5,"key":"string","value-id":6,"value":"long"}`,
			want: "map<string, long>",
		},
		{
			name: "struct renders on one line",
			raw: `{"type":"struct","fields":[
				{"id":7,"name":"city","required":false,"type":"string"},
				{"id":8,"name":"zip","required":false,"type":"int"}]}`,
			want: "struct<city: string, zip: int>",
		},
		{
			name: "nested",
			raw: `{"type":"list","element-id":9,"element":
				{"type":"struct","fields":[{"id":10,"name":"k","required":true,"type":"string"}]}}`,
			want: "list<struct<k: string>>",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var value any
			if err := json.Unmarshal([]byte(test.raw), &value); err != nil {
				t.Fatalf("unmarshalling the fixture: %v", err)
			}

			if got := TypeString(value); got != test.want {
				t.Errorf("TypeString() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestFormatTransformed(t *testing.T) {
	tests := []struct {
		transform string
		value     any
		want      string
	}{
		// The time transforms store a count from the epoch, which is what makes them worth
		// turning back.
		{"year", 56, "2026"},
		{"month", 679, "2026-08"},
		{"day", 20681, "2026-08-16"},
		{"hour", 496_355, "2026-08-16 11"},
		// Everything else is shown as stored.
		{"identity", "eu-central-1", "eu-central-1"},
		{"bucket[16]", 7, "7"},
		{"truncate[4]", "abcd", "abcd"},
		{"void", nil, "null"},
		// A transform naming a time but carrying something that is not a count leaves the value
		// alone rather than turning nonsense into a date.
		{"day", "2026-08-16", "2026-08-16"},
		// A day is written with avro's date logical type, so a decoder may hand back a time. It
		// is rendered to the day the transform reaches and no further.
		{"day", time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC), "2026-08-16"},
		{"hour", time.Date(2026, 8, 16, 11, 0, 0, 0, time.UTC), "2026-08-16 11"},
		// A time under a transform that is not a time transform keeps its whole value: nothing
		// says which part of it means anything.
		{"identity", time.Date(2026, 8, 16, 11, 2, 3, 0, time.UTC), "2026-08-16 11:02:03"},
	}

	for _, test := range tests {
		t.Run(test.transform, func(t *testing.T) {
			if got := FormatTransformed(test.transform, test.value); got != test.want {
				t.Errorf(
					"FormatTransformed(%q, %v) = %q, want %q",
					test.transform, test.value, got, test.want,
				)
			}
		})
	}
}

func TestFormatPartition(t *testing.T) {
	fields := []PartitionField{
		{SourceID: 2, FieldID: 1000, Name: "dt", Transform: "day"},
		{SourceID: 3, FieldID: 1001, Name: "region", Transform: "identity"},
	}

	t.Run("under the spec the manifest declares", func(t *testing.T) {
		got := FormatPartition(fields, map[string]any{"dt": 20681, "region": "eu"})
		want := "dt=2026-08-16, region=eu"
		if got != want {
			t.Errorf("FormatPartition() = %q, want %q", got, want)
		}
	})

	t.Run("fields keep the order the spec gives them", func(t *testing.T) {
		got := FormatPartition(fields, map[string]any{"region": "eu", "dt": 20681})
		want := "dt=2026-08-16, region=eu"
		if got != want {
			t.Errorf("FormatPartition() = %q, want %q", got, want)
		}
	})

	t.Run("a value with no field in the spec is left out", func(t *testing.T) {
		got := FormatPartition(fields[:1], map[string]any{"dt": 20681, "region": "eu"})
		want := "dt=2026-08-16"
		if got != want {
			t.Errorf("FormatPartition() = %q, want %q", got, want)
		}
	})

	t.Run("no spec shows the values as stored, in name order", func(t *testing.T) {
		got := FormatPartition(nil, map[string]any{"region": "eu", "dt": 20681})
		want := "dt=20681, region=eu"
		if got != want {
			t.Errorf("FormatPartition() = %q, want %q", got, want)
		}
	})

	t.Run("an unpartitioned file renders as nothing", func(t *testing.T) {
		if got := FormatPartition(fields, nil); got != "" {
			t.Errorf("FormatPartition() = %q, want empty", got)
		}
	})
}
