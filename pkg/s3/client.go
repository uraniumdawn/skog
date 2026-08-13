// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

// Package s3 wraps the AWS S3 API with the few operations skog needs, in the shape the UI
// consumes them: a bucket list, one level of a key hierarchy, and an object's metadata.
package s3

import (
	"context"
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/uraniumdawn/skog/pkg/awscfg"
	"github.com/uraniumdawn/skog/pkg/util"
)

// Delimiter separates the levels of a key hierarchy. S3 keys are flat; this is the character
// that makes "data/time/12.json" look like a path, both to S3 (as a listing delimiter) and to
// the user.
const Delimiter = "/"

// Client is an S3 client bound to one AWS profile.
type Client struct {
	api     *awss3.Client
	profile *awscfg.Profile

	// maxEntries is how many entries one request asks for — S3 caps a response at a thousand
	// however much is asked for — and maxScannedKeys caps a recursive aggregate.
	maxEntries     int32
	maxScannedKeys int
}

// Settings are the limits skog applies to S3 listings.
type Settings struct {
	// MaxRequestedEntries is how many entries one request asks for. S3 caps a ListObjectsV2
	// response at a thousand entries, so anything above that yields a thousand.
	MaxRequestedEntries int
	// MaxScannedKeys caps the keys a recursive aggregate walks; zero means no cap.
	MaxScannedKeys int
}

// NewClient builds an S3 client for the given profile. Credentials are resolved by the AWS
// SDK for that profile name, so static keys, SSO, and assumed roles all work.
//
// A profile with an endpoint_url is treated as an S3-compatible service (MinIO, LocalStack,
// Ceph) and addressed path-style, since virtual-host addressing would need per-bucket DNS.
func NewClient(
	ctx context.Context,
	profile *awscfg.Profile,
	settings Settings,
) (*Client, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithSharedConfigProfile(profile.Name))
	if err != nil {
		return nil, fmt.Errorf("loading aws profile %q: %w", profile.Name, err)
	}

	// S3 signs every request with a region. Neither the profile nor the environment naming
	// one is fatal for an S3-compatible endpoint, which ignores the value.
	if cfg.Region == "" {
		cfg.Region = profile.EffectiveRegion()
	}

	api := awss3.NewFromConfig(cfg, func(o *awss3.Options) {
		if profile.IsCustomEndpoint() {
			o.BaseEndpoint = aws.String(profile.EndpointURL)
			o.UsePathStyle = true
		}
	})

	return &Client{
		api:            api,
		profile:        profile,
		maxEntries:     int32(settings.MaxRequestedEntries),
		maxScannedKeys: settings.MaxScannedKeys,
	}, nil
}

// Profile returns the profile the client is bound to.
func (c *Client) Profile() *awscfg.Profile {
	return c.profile
}

// Bucket is one entry of the bucket list.
type Bucket struct {
	Name      string
	CreatedAt time.Time
	Region    string
}

// ListBuckets returns the buckets visible to the profile, in the order S3 reports them
// (alphabetical by name).
func (c *Client) ListBuckets(ctx context.Context) ([]Bucket, error) {
	out, err := c.api.ListBuckets(ctx, &awss3.ListBucketsInput{})
	if err != nil {
		return nil, err
	}

	buckets := make([]Bucket, 0, len(out.Buckets))
	for _, b := range out.Buckets {
		buckets = append(buckets, Bucket{
			Name:      aws.ToString(b.Name),
			CreatedAt: aws.ToTime(b.CreationDate),
			Region:    aws.ToString(b.BucketRegion),
		})
	}
	return buckets, nil
}

// Object is one key of a listing.
type Object struct {
	Key          string
	Size         int64
	LastModified time.Time
	StorageClass string
}

// Listing is one batch of one level of a bucket's key hierarchy: the prefixes ("folders")
// directly under the requested prefix, and the keys that live in it.
type Listing struct {
	Bucket   string
	Prefix   string
	Prefixes []string // full prefixes, each ending in Delimiter
	Objects  []Object // full keys
	// NextToken continues the level where this batch stopped, and is empty once the level holds
	// nothing more.
	NextToken string
}

// Total returns the number of entries in the listing.
func (l *Listing) Total() int {
	return len(l.Prefixes) + len(l.Objects)
}

// ListObjects returns the first batch of bucket's hierarchy under prefix. prefix is empty for
// the root of the bucket and otherwise ends with Delimiter.
//
// A batch is one request, so a prefix with a million keys under it is not loaded at once. What is
// left is reached with ListMore; see Listing.NextToken.
func (c *Client) ListObjects(ctx context.Context, bucket, prefix string) (*Listing, error) {
	return listObjects(ctx, c.api, listRequest{
		bucket:     bucket,
		prefix:     prefix,
		maxEntries: c.maxEntries,
	})
}

// ListMore returns the batch that continues a level, from the token of the batch before it.
func (c *Client) ListMore(
	ctx context.Context,
	bucket, prefix, token string,
) (*Listing, error) {
	return listObjects(ctx, c.api, listRequest{
		bucket:     bucket,
		prefix:     prefix,
		token:      token,
		maxEntries: c.maxEntries,
	})
}

// listRequest holds the parameters of one batch, keeping listObjects's signature readable.
type listRequest struct {
	bucket     string
	prefix     string
	token      string
	maxEntries int32
}

// listObjects is one listing request against any page source, so batching can be tested without
// an S3.
func listObjects(
	ctx context.Context,
	lister awss3.ListObjectsV2APIClient,
	req listRequest,
) (*Listing, error) {
	input := &awss3.ListObjectsV2Input{
		Bucket:    aws.String(req.bucket),
		Prefix:    aws.String(req.prefix),
		Delimiter: aws.String(Delimiter),
		MaxKeys:   aws.Int32(req.maxEntries),
	}
	if req.token != "" {
		input.ContinuationToken = aws.String(req.token)
	}

	page, err := lister.ListObjectsV2(ctx, input)
	if err != nil {
		return nil, err
	}

	listing := &Listing{Bucket: req.bucket, Prefix: req.prefix}
	for _, p := range page.CommonPrefixes {
		listing.Prefixes = append(listing.Prefixes, aws.ToString(p.Prefix))
	}
	for _, o := range page.Contents {
		key := aws.ToString(o.Key)
		// The prefix itself shows up as a zero-byte key when it was created as a folder
		// marker; it is the level being listed, not an entry in it.
		if key == req.prefix {
			continue
		}
		listing.Objects = append(listing.Objects, Object{
			Key:          key,
			Size:         aws.ToInt64(o.Size),
			LastModified: aws.ToTime(o.LastModified),
			StorageClass: string(o.StorageClass),
		})
	}

	// S3 only sends a continuation token while there is more to come, so an empty one means the
	// level ends with this batch, however few entries it gathered.
	listing.NextToken = aws.ToString(page.NextContinuationToken)
	return listing, nil
}

// ObjectInfo is an object's metadata, as returned by HeadObject.
type ObjectInfo struct {
	Bucket               string
	Key                  string
	Size                 int64
	LastModified         time.Time
	ContentType          string
	ContentEncoding      string
	CacheControl         string
	StorageClass         string
	ETag                 string
	VersionID            string
	ServerSideEncryption string
	Metadata             map[string]string
}

// HeadObject returns the metadata of a single object without fetching its body.
func (c *Client) HeadObject(ctx context.Context, bucket, key string) (*ObjectInfo, error) {
	out, err := c.api.HeadObject(ctx, &awss3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, err
	}

	return &ObjectInfo{
		Bucket:               bucket,
		Key:                  key,
		Size:                 aws.ToInt64(out.ContentLength),
		LastModified:         aws.ToTime(out.LastModified),
		ContentType:          aws.ToString(out.ContentType),
		ContentEncoding:      aws.ToString(out.ContentEncoding),
		CacheControl:         aws.ToString(out.CacheControl),
		StorageClass:         string(out.StorageClass),
		ETag:                 strings.Trim(aws.ToString(out.ETag), `"`),
		VersionID:            aws.ToString(out.VersionId),
		ServerSideEncryption: string(out.ServerSideEncryption),
		Metadata:             out.Metadata,
	}, nil
}

// DeleteObject removes a single object.
//
// On a versioned bucket this writes a delete marker rather than removing the data: the object
// disappears from a listing, but its versions remain until they are deleted explicitly.
func (c *Client) DeleteObject(ctx context.Context, bucket, key string) error {
	_, err := c.api.DeleteObject(ctx, &awss3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	return err
}

// String renders the object metadata for the description page.
func (o *ObjectInfo) String() string {
	var sb strings.Builder
	w := tabwriter.NewWriter(&sb, 0, 0, 2, ' ', 0)

	_, _ = fmt.Fprintf(w, "Bucket:\t%s\n", o.Bucket)
	_, _ = fmt.Fprintf(w, "Key:\t%s\n", o.Key)
	_, _ = fmt.Fprintf(w, "Size:\t%s (%d bytes)\n", util.FormatBytes(o.Size), o.Size)
	_, _ = fmt.Fprintf(w, "Last modified:\t%s\n", FormatTime(o.LastModified))

	writeIfSet(w, "Content type", o.ContentType)
	writeIfSet(w, "Content encoding", o.ContentEncoding)
	writeIfSet(w, "Cache control", o.CacheControl)
	writeIfSet(w, "Storage class", o.StorageClass)
	writeIfSet(w, "ETag", o.ETag)
	writeIfSet(w, "Version", o.VersionID)
	writeIfSet(w, "Encryption", o.ServerSideEncryption)

	if len(o.Metadata) > 0 {
		_, _ = fmt.Fprintln(w, "")
		_, _ = fmt.Fprintf(w, "Metadata:\t%d\n", len(o.Metadata))
		for _, key := range util.SortedKeys(o.Metadata) {
			_, _ = fmt.Fprintf(w, "  %s:\t%s\n", key, o.Metadata[key])
		}
	}

	_ = w.Flush()
	return sb.String()
}

func writeIfSet(w *tabwriter.Writer, label, value string) {
	if value == "" {
		return
	}
	_, _ = fmt.Fprintf(w, "%s:\t%s\n", label, value)
}

// TimeLayout is how skog renders S3 timestamps: local time, seconds precision.
const TimeLayout = "2006-01-02 15:04:05"

// FormatTime renders a timestamp for display, blank for the zero time.
func FormatTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Local().Format(TimeLayout)
}
