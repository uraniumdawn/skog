// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package s3

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// downloader serves canned listings and object bodies, so a download can be tested without an S3.
type downloader struct {
	pagedLister
	// bodies is the content of each key; a key with no entry fails the way a missing object does.
	bodies map[string]string
	// broken is the key whose body fails halfway through, standing for a connection that drops.
	broken string
	got    []string // the keys asked for, in order
}

func (d *downloader) GetObject(
	_ context.Context,
	in *awss3.GetObjectInput,
	_ ...func(*awss3.Options),
) (*awss3.GetObjectOutput, error) {
	key := aws.ToString(in.Key)
	d.got = append(d.got, key)

	if key == d.broken {
		return &awss3.GetObjectOutput{Body: io.NopCloser(brokenBody{})}, nil
	}
	body, ok := d.bodies[key]
	if !ok {
		return nil, errors.New("NoSuchKey: " + key)
	}
	return &awss3.GetObjectOutput{Body: io.NopCloser(strings.NewReader(body))}, nil
}

// brokenBody hands out some bytes and then fails, the way a dropped connection does.
type brokenBody struct{}

var errBodyDropped = errors.New("connection reset")

func (brokenBody) Read(p []byte) (int, error) {
	copy(p, "half")
	return 4, errBodyDropped
}

func key(name string) types.Object {
	return object(name, 0, time.Date(2026, 8, 10, 11, 2, 14, 0, time.UTC), types.ObjectStorageClassStandard)
}

func TestDownloadPrefixKeepsTheKeyHierarchy(t *testing.T) {
	dir := t.TempDir()
	api := &downloader{
		pagedLister: pagedLister{pages: [][]types.Object{
			{key("data/"), key("data/12.json")},
			{key("data/time/13.json")},
		}},
		bodies: map[string]string{
			"data/":             "",
			"data/12.json":      "twelve",
			"data/time/13.json": "thirteen",
		},
	}

	var progress []int
	result, err := downloadPrefix(context.Background(), api, downloadRequest{
		bucket: "bucket", prefix: "data/", dir: dir, maxEntries: 1000,
		onObject: func(done int, _ string) { progress = append(progress, done) },
	})
	if err != nil {
		t.Fatalf("downloadPrefix() error = %v", err)
	}

	// The folder marker is the level itself, not content in it: it must not be fetched, nor
	// written as a file where the folder has to go.
	for _, asked := range api.got {
		if asked == "data/" {
			t.Errorf("the folder marker was fetched, keys asked for = %v", api.got)
		}
	}
	if result.Objects != 2 || result.Bytes != int64(len("twelve")+len("thirteen")) {
		t.Errorf("result = %d objects, %d bytes, want 2 objects, 14 bytes",
			result.Objects, result.Bytes)
	}
	if result.Partial {
		t.Error("result is partial, want the whole prefix downloaded")
	}
	if want := filepath.Join(dir, "bucket", "data"); result.Path != want {
		t.Errorf("Path = %q, want %q", result.Path, want)
	}
	if len(progress) != 2 || progress[1] != 2 {
		t.Errorf("progress = %v, want one report per object", progress)
	}

	for path, want := range map[string]string{
		filepath.Join(dir, "bucket", "data", "12.json"):         "twelve",
		filepath.Join(dir, "bucket", "data", "time", "13.json"): "thirteen",
	} {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) error = %v", path, err)
		}
		if string(got) != want {
			t.Errorf("%q holds %q, want %q", path, got, want)
		}
	}
}

// The cap is what keeps a mistyped folder from spending a day of requests, so a download has to
// respect it and say that it did.
func TestDownloadPrefixStopsAtTheScannedKeysCap(t *testing.T) {
	dir := t.TempDir()
	api := &downloader{
		pagedLister: pagedLister{pages: [][]types.Object{
			{key("a.json"), key("b.json")},
			{key("c.json")},
		}},
		bodies: map[string]string{"a.json": "a", "b.json": "b", "c.json": "c"},
	}

	result, err := downloadPrefix(context.Background(), api, downloadRequest{
		bucket: "bucket", dir: dir, maxEntries: 1000, maxScanned: 2,
	})
	if err != nil {
		t.Fatalf("downloadPrefix() error = %v", err)
	}

	if result.Objects != 2 || !result.Partial {
		t.Errorf("result = %d objects, partial = %v, want 2 and true",
			result.Objects, result.Partial)
	}
	if _, err := os.Stat(filepath.Join(dir, "bucket", "c.json")); !os.IsNotExist(err) {
		t.Error("a key past the cap was downloaded")
	}
}

// A download that fails partway must leave nothing behind that reads as a whole object.
func TestDownloadObjectLeavesNoHalfObject(t *testing.T) {
	dir := t.TempDir()
	api := &downloader{bodies: map[string]string{"a.json": "a"}, broken: "a.json"}

	path := filepath.Join(dir, "bucket", "a.json")
	if _, err := downloadObject(context.Background(), api, "bucket", "a.json", path); !errors.Is(
		err, errBodyDropped,
	) {
		t.Fatalf("downloadObject() error = %v, want %v", err, errBodyDropped)
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("the target file exists, want a failed download to leave none")
	}
	left, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	if len(left) != 0 {
		t.Errorf("the download folder holds %d files, want the temporary one cleaned up", len(left))
	}
}

func TestLocalPath(t *testing.T) {
	root := filepath.Join("downloads", "bucket")

	tests := []struct {
		name    string
		key     string
		want    string
		wantErr bool
	}{
		{
			name: "a key becomes the path its delimiters describe",
			key:  "data/time/12.json",
			want: filepath.Join(root, "data", "time", "12.json"),
		},
		{
			name: "an empty prefix is the bucket's own folder",
			key:  "",
			want: root,
		},
		{
			// A key is arbitrary text: without this, a listing could write over anything the user
			// can write to.
			name:    "a key climbing out of the folder is refused",
			key:     "../../etc/passwd",
			wantErr: true,
		},
		{
			name:    "a key climbing out from inside is refused",
			key:     "data/../../elsewhere/12.json",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := localPath("downloads", "bucket", tt.key)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("localPath(%q) = %q, want an error", tt.key, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("localPath(%q) error = %v", tt.key, err)
			}
			if got != tt.want {
				t.Errorf("localPath(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}
