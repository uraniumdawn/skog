// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package iceberg

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/uraniumdawn/skog/pkg/util"
)

// TypeString renders an Iceberg type the way the spec writes it.
//
// A primitive is a string in the metadata file and comes back as it was written. A nested type
// is a document, and is rendered in one line — a schema is read down its name column, and a
// struct spread over four lines pushes the fields under it off the screen.
func TypeString(t any) string {
	switch v := t.(type) {
	case string:
		return v
	case map[string]any:
		switch str(v["type"]) {
		case "struct":
			return "struct<" + strings.Join(fieldTypes(v["fields"]), ", ") + ">"
		case "list":
			return "list<" + TypeString(v["element"]) + ">"
		case "map":
			return "map<" + TypeString(v["key"]) + ", " + TypeString(v["value"]) + ">"
		}
		return str(v["type"])
	case nil:
		return ""
	default:
		return fmt.Sprint(v)
	}
}

// fieldTypes renders the fields of a struct as "name: type".
func fieldTypes(fields any) []string {
	list, ok := fields.([]any)
	if !ok {
		return nil
	}

	out := make([]string, 0, len(list))
	for _, f := range list {
		field, ok := f.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, str(field["name"])+": "+TypeString(field["type"]))
	}
	return out
}

// FormatPartition renders the partition of a data file as the columns it is partitioned by,
// under the transforms the spec declares for them: "dt=2026-08-16, region=eu".
//
// A partition value is stored as what the transform produces — a day is an integer count of days
// — so the transform is what makes the value readable. Fields with no matching value, and values
// with no matching field, are left out: a manifest written under one spec and read under another
// is the one case where they disagree.
func FormatPartition(fields []PartitionField, values map[string]any) string {
	if len(values) == 0 {
		return ""
	}

	// A manifest that carries no spec of its own leaves the values as they were stored, in the
	// order avro wrote them.
	if len(fields) == 0 {
		return formatUnknownPartition(values)
	}

	parts := make([]string, 0, len(fields))
	for _, field := range fields {
		value, ok := values[field.Name]
		if !ok {
			continue
		}
		parts = append(parts, field.Name+"="+FormatTransformed(field.Transform, value))
	}
	return strings.Join(parts, ", ")
}

// formatUnknownPartition renders partition values with no spec to read them by, in name order so
// the same file renders the same way twice.
func formatUnknownPartition(values map[string]any) string {
	names := util.SortedKeys(values)

	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, name+"="+FormatValue(values[name]))
	}
	return strings.Join(parts, ", ")
}

// FormatTransformed renders one partition value under the transform that produced it.
//
// A time transform stores a count from the epoch — year 56 is 2026, day 20681 is 2026-08-16 —
// and the count is what makes it worth turning back. A day is written with avro's date logical
// type, so a decoder may hand back a time instead of the count; either is rendered to the
// granularity the transform has, and no further. Anything else is shown as it is stored, which
// for an identity partition is the value itself.
func FormatTransformed(transform string, value any) string {
	layout, ok := transformLayouts[strings.ToLower(transform)]
	if !ok {
		return FormatValue(value)
	}

	if t, ok := value.(time.Time); ok {
		return t.UTC().Format(layout)
	}

	n, ok := asInt64(value)
	if !ok {
		return FormatValue(value)
	}

	epoch := time.Unix(0, 0).UTC()
	switch strings.ToLower(transform) {
	case "year":
		return strconv.Itoa(1970 + int(n))
	case "month":
		return epoch.AddDate(0, int(n), 0).Format(layout)
	case "day":
		return epoch.AddDate(0, 0, int(n)).Format(layout)
	default:
		return epoch.Add(time.Duration(n) * time.Hour).Format(layout)
	}
}

// transformLayouts is how far down a time transform reaches, and so how much of a date it is
// worth showing: a day partition rendered with an hour on it says something the value does not.
var transformLayouts = map[string]string{
	"year":  "2006",
	"month": "2006-01",
	"day":   "2006-01-02",
	"hour":  "2006-01-02 15",
}

// FormatValue renders a value decoded out of an avro file for display.
func FormatValue(value any) string {
	switch v := value.(type) {
	case nil:
		return "null"
	case []byte:
		return hex.EncodeToString(v)
	case string:
		return v
	case time.Time:
		return v.UTC().Format("2006-01-02 15:04:05")
	case bool:
		return strconv.FormatBool(v)
	case float32:
		return strconv.FormatFloat(float64(v), 'g', -1, 32)
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64)
	default:
		if n, ok := asInt64(v); ok {
			return strconv.FormatInt(n, 10)
		}
		return fmt.Sprint(v)
	}
}

// asInt64 widens whichever integer type the avro decoder produced.
func asInt64(value any) (int64, bool) {
	switch v := value.(type) {
	case int:
		return int64(v), true
	case int32:
		return int64(v), true
	case int64:
		return v, true
	default:
		return 0, false
	}
}

// str is the string a decoded value holds, empty for anything else.
func str(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}
