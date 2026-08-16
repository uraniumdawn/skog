// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package iceberg

import "testing"

func TestParseLocation(t *testing.T) {
	const base = "s3://warehouse/db/orders"

	tests := []struct {
		name       string
		path       string
		base       string
		wantBucket string
		wantKey    string
		wantErr    bool
	}{
		{
			name:       "s3 uri",
			path:       "s3://warehouse/db/orders/metadata/snap-1.avro",
			wantBucket: "warehouse",
			wantKey:    "db/orders/metadata/snap-1.avro",
		},
		{
			// Spark and Hive write s3a:// for the same storage.
			name:       "s3a uri",
			path:       "s3a://warehouse/db/orders/data/f.parquet",
			wantBucket: "warehouse",
			wantKey:    "db/orders/data/f.parquet",
		},
		{
			name:       "s3n uri",
			path:       "s3n://warehouse/f.parquet",
			wantBucket: "warehouse",
			wantKey:    "f.parquet",
		},
		{
			name:       "bucket alone",
			path:       "s3://warehouse",
			wantBucket: "warehouse",
			wantKey:    "",
		},
		{
			name:       "relative path resolves against the table",
			path:       "metadata/00001-abc.metadata.json",
			base:       base,
			wantBucket: "warehouse",
			wantKey:    "db/orders/metadata/00001-abc.metadata.json",
		},
		{
			name:       "relative path with a leading delimiter",
			path:       "/metadata/x.avro",
			base:       base + "/",
			wantBucket: "warehouse",
			wantKey:    "db/orders/metadata/x.avro",
		},
		{
			name:    "another storage is refused rather than guessed at",
			path:    "hdfs://nn/warehouse/db/orders/data/f.parquet",
			base:    base,
			wantErr: true,
		},
		{
			name:    "relative path with no table location",
			path:    "metadata/x.avro",
			wantErr: true,
		},
		{
			name:    "empty",
			path:    "   ",
			base:    base,
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			bucket, key, err := ParseLocation(test.path, test.base)

			if test.wantErr {
				if err == nil {
					t.Fatalf("ParseLocation(%q) = %q, %q; want an error", test.path, bucket, key)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseLocation(%q): %v", test.path, err)
			}
			if bucket != test.wantBucket || key != test.wantKey {
				t.Errorf(
					"ParseLocation(%q) = %q, %q; want %q, %q",
					test.path, bucket, key, test.wantBucket, test.wantKey,
				)
			}
		})
	}
}
