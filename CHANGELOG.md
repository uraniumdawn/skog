# Changelog

All notable changes to this project will be documented in this file.

---

## [0.0.1] - 2026-08-16

First release.

## Features

* **AWS profiles, read from where they already are**: the Profiles page lists what `~/.aws/config` and `~/.aws/credentials` declare — name, region, endpoint, whether the profile carries credentials of its own, and which files declared it. Credentials are resolved by the AWS SDK, so static keys, SSO and assumed roles all work. skog never writes to those files. `Enter` picks the profile every S3 operation runs against, and the choice is remembered in `config.yaml` for the next start.
* **S3-compatible endpoints**: a profile with `endpoint_url` — set directly, through a `services` section, or in `AWS_ENDPOINT_URL_S3`/`AWS_ENDPOINT_URL` — is addressed path-style, which is what MinIO, LocalStack and Ceph need without per-bucket DNS.
* **Buckets and the key hierarchy**: S3 keys are flat, so a "folder" is the part of a key up to the next `/`. The objects page shows one level: the folders directly under the prefix first, then the objects in it. A level arrives in batches of `s3.max_requested_entries`, and `<n>` asks for the next one — the key is on the menu only while there is one to ask for.
* **Navigation by hierarchy**: `<h>` and `<l>` walk the levels — profiles, buckets, a prefix, the prefix under it, an object's metadata, what the object holds, one row of it in full. Where a level is is computed from the key itself rather than from the order pages were opened in, so a prefix reached by any route knows what is above it.
* **Aggregates on demand** (`<i>`): S3 says nothing about what a prefix holds until it is walked, so a folder starts with unknown figures and `<i>` fills them in place — objects, total size, last modified, storage class. The walk is capped at `s3.max_scanned_keys` and an aggregate that hits the cap is marked with `>`, its figures being a floor rather than a total. `<Esc>` ends a long one. On an object `<i>` opens its metadata instead, so the key means the same thing on every row.
* **Object metadata**: size, last modified, content type and encoding, cache control, storage class, ETag, version, server-side encryption and user metadata — read with a single `HeadObject`, the body never fetched.
* **A viewer for what an object holds**: Parquet and Avro become a table under the columns their schema names, `.jsonl` and `.ndjson` are shown a record to a line as they were written, and a `.json` document is laid out for reading. Any of them but Parquet may be gzipped. The format is taken from the extension and checked against the file's own first bytes, so a mislabelled key says so instead of failing as a parse error. Rows arrive `viewer.page_rows` at a time, `<n>` reads the next batch, `<s>` shows the schema the file declares, and `<l>` opens the row under the cursor in full — a field to a line, for values wider than the terminal.
* **A body cache** (`cache.dir`): an object opened a second time costs no transfer. An entry is named for a digest of the profile, bucket, key and ETag it was fetched with, so a changed object is a different entry and is fetched again rather than read stale. Over `cache.max_size_mb` the object viewed longest ago goes first.
* **Download** (`<d>`): an object lands in `<download.dir>/<bucket>/<key>`; on a folder row it asks first, then brings every key under that prefix along, hierarchy and all. Bodies are written through a temporary file and renamed into place, so a cancelled download leaves no half-written object. Progress is reported as it goes and `<Esc>` ends it.
* **Delete** (`<x>`): an object, or a folder and everything under it. A recursive delete costs one request per thousand keys rather than one per key, takes the folder markers with it, and walks under no scanned-keys cap — stopping early would leave a level half removed while reporting it gone. A prefix that does not name a level is refused outright, since `data/2024` covers `data/2024-backup/` as much as `data/2024/`.
* **Three modes, per profile** (`<Tab>` on the Profiles page): `read-only` refuses every modifying action, `regular` asks in the status line first, `yolo` runs it on the spot. The mode is a property of the profile, not of the session, so a production profile can be pinned to `read-only` and a local one left on `yolo`. The badge on the content border says which one is in force, red on `yolo`.
* **Key reference** (`<?>`): every key of the application, grouped by what it acts on. The keys the page in front offers are on the bottom bar at all times.
* **Search** (`</>`): fuzzy-matches the rows of the page. `<Enter>` keeps the filter and hands the keyboard back to the list, `<Esc>` drops it; a filter you keep survives navigating away and back, and the page title carries it.
* **One background job at a time**: an aggregate, a download and a delete each hold the job slot for as long as they run, so `<Esc>` is unambiguous about what it ends and the user's requests are not spent on rows they have stopped looking at.
* **Pages are cached for the session**: a level you have already opened comes back instantly and without a request. `<C-u>` fetches it again.
* **Styles**: colors come from a YAML file merged on top of the built-in defaults, so a theme only names what it changes. Eleven ready-made ones are in [`examples/style/`](examples/style/README.md).

---
