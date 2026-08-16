# skog 🌲

A terminal UI for S3. Browse buckets, walk a key hierarchy, read what an object holds, download
it or delete it — from the keyboard, without leaving the terminal.

![License](https://img.shields.io/badge/license-MIT-blue.svg)
![Go Version](https://img.shields.io/badge/go-%3E%3D1.25-blue)

## Table of Contents

- [Features](#features)
- [Available Pages](#available-pages)
- [Installation](#installation)
- [Getting Started](#getting-started)
- [Configuration](#configuration)
- [Development](#development)
- [License](#license)
- [Acknowledgments](#acknowledgments)
- [Support](#support)

## Features

- **Your AWS profiles, where they already are** — the Profiles page lists what `~/.aws/config` and `~/.aws/credentials` declare. Credentials are resolved by the AWS SDK, so static keys, SSO and assumed roles all work. skog never writes to those files; the profile you select is remembered in its own `config.yaml`.
- **S3-compatible endpoints** — a profile with `endpoint_url`, a `services` section, or `AWS_ENDPOINT_URL_S3`/`AWS_ENDPOINT_URL` in the environment is addressed path-style, which is what MinIO, LocalStack and Ceph need without per-bucket DNS.
- **Navigation by hierarchy** — `h` and `l` walk the levels: profiles, buckets, a prefix, the prefix under it, an object's metadata, what the object holds, one row of it in full. Where a level sits is computed from the key itself, not from the order pages were opened in, so a prefix reached by any route knows what is above it.
- **One level at a time** — S3 keys are flat, so a "folder" is the part of a key up to the next `/`. A level arrives in batches of `s3.max_requested_entries` and `n` asks for the next one, on the menu only while there is one to ask for.
- **Aggregates on demand** — S3 says nothing about what a prefix holds until it is walked, so a folder starts with unknown figures and `i` fills them in place: objects, total size, last modified, storage class. The walk stops at `s3.max_scanned_keys` and an aggregate that hit the cap is marked with `>`, its figures a floor rather than a total. `Esc` ends a long one. On an object row `i` opens its metadata instead, so the key means the same thing on every row.
- **Object metadata** — size, last modified, content type and encoding, cache control, storage class, ETag, version, encryption and user metadata, read with a single `HeadObject`. The body is never fetched for this.
- **A viewer for the object itself** — Parquet and Avro become a table under the columns their schema names; `.jsonl` and `.ndjson` are shown a record to a line, as they were written; a `.json` document is laid out for reading. Any of them but Parquet may be gzipped (`events.jsonl.gz`). The format comes from the extension and is checked against the file's own first bytes, so a mislabelled key says so instead of failing as a parse error.
- **Rows in batches** — the viewer holds `viewer.page_rows` at a time and `n` reads the next batch off the local file. `s` shows the schema the file declares, and `l` opens the row under the cursor in full, a field to a line — the one place values wider than the terminal are readable.
- **A body cache** — an object opened a second time costs no transfer. An entry is named for a digest of the profile, bucket, key and ETag it was fetched with, so a changed object is a different entry and is fetched again rather than read stale. Over `cache.max_size_mb` the object viewed longest ago goes first.
- **Download** — `d` writes an object to `<download.dir>/<bucket>/<key>`; on a folder row it asks first, then brings every key under that prefix along, hierarchy and all. Bodies go through a temporary file and are renamed into place, so a cancelled download leaves no half-written object.
- **Delete** — `x` removes an object, or a folder and everything under it. A recursive delete costs one request per thousand keys, takes the folder markers with it, and runs under no scanned-keys cap: stopping early would leave a level half removed while reporting it gone. A prefix that does not name a level is refused, because `data/2024` covers `data/2024-backup/` as much as `data/2024/`.
- **Three modes, per profile** — `read-only` refuses every modifying action, `regular` asks in the status line first, `yolo` asks nothing. `Tab` on the Profiles page cycles the highlighted profile, and the badge on the content border says which mode is in force — red on `yolo`. **Keep production on `read-only` or `regular`: in `yolo` an object, or a whole prefix, is gone the moment the key lands, and S3 has nothing to roll back to.**
- **Key reference** — `?` lists every key of the application, grouped by what it acts on. The keys the page in front offers are on the bottom bar at all times.
- **Search** — `/` fuzzy-matches the rows of the page. `Enter` keeps the filter and hands the keyboard back to the list, `Esc` drops it. A filter you keep survives navigating away and back, and the page title carries it.
- **One background job at a time** — an aggregate, a download and a delete each hold the job slot for as long as they run, so `Esc` is unambiguous about which one it ends, and your requests are not spent on rows you have stopped looking at.
- **Pages cached for the session** — a level you have already opened comes back instantly and without a request; `Ctrl+U` fetches it again.
- **Styles** — colors come from a YAML file merged on top of the built-in defaults, so a theme names only what it changes. Eleven ready-made ones are in [`examples/style/`](examples/style/README.md).

### Key conventions

Keys mean the same thing wherever they appear:

| Key | Means |
|-----|-------|
| `h` / `l` | A level up, or into the row under the cursor |
| `j` / `k`, `↑` / `↓` | Move between rows |
| `g` / `G` | First row, last row |
| `Ctrl+F` / `Ctrl+B` | Page down, page up |
| `H` / `L` | Scroll a wide page sideways |
| `i` | What the row holds — a folder is walked, an object opened |
| `d` | Download it |
| `x` | Delete it **in S3** |
| `n` | Load the next batch |
| `s` | The schema the file being viewed declares |
| `/` | Filter the rows, `Esc` clears the filter |
| `:` | Resource menu |
| `Ctrl+U` | Fetch the page again |
| `Esc` | Cancel the running job, or close a modal |
| `Tab` | Switch a profile's mode, on the Profiles page |
| `Y` / `N` | Answer the question in the status line |
| `?` | Every key, in one list |
| `Ctrl+C` | Quit |

## Available Pages

Press `:` for the resource menu: **Profiles** and **S3**. Everything else is reached by walking
down from them with `l`.

| Page | Shows | Keys it adds |
|------|-------|--------------|
| **Profiles** | The profiles in `~/.aws`, with region, endpoint, credentials, source and mode | `Enter` select, `Tab` mode |
| **Buckets** | The buckets the profile can see, with creation date and region | `i` aggregate |
| **Objects** | One level of a bucket's key hierarchy, folders first | `i` aggregate or metadata, `d` download, `x` delete, `n` next batch |
| **Object** | An object's metadata | `H`/`L` scroll |
| **Data** | What the object holds, as a table or as its lines | `n` next batch, `s` schema, `H`/`L` scroll |
| **Record** | One row of the table, a field to a line | `Esc` close |
| **Schema** | The schema the file declares | `H`/`L` scroll |

## Installation

### From source

```bash
git clone https://github.com/uraniumdawn/skog.git
cd skog

go build -o skog

mv skog /usr/local/bin/
```

No cgo, no system libraries: the AWS SDK, the Parquet reader and the Avro reader are all Go.

## Getting Started

### 1. Have an AWS profile

skog reads `~/.aws/config` and `~/.aws/credentials` and never writes to them. Any profile the AWS
SDK can resolve works — static keys, SSO, an assumed role. A profile pointing at something other
than AWS names its endpoint:

```ini
[profile minio-local]
region = us-east-1
endpoint_url = http://localhost:9000

[profile prod]
region = eu-central-1
sso_session = company
sso_account_id = 123456789012
sso_role_name = ReadOnly
```

### 2. Run it

```bash
skog
```

It starts on the Profiles page. `Enter` selects the profile to work against — it is remembered,
so the next start comes up on it — and `l` from there opens its buckets. `?` lists every key.
`Ctrl+C` quits, from anywhere.

To check the version:

```bash
skog -version
```

## Configuration

### config.yaml

skog keeps its own settings in `~/.config/skog/config.yaml`, apart from the AWS configuration it
only reads. Every key has a built-in default, so the file is optional and holds only what you
change:

```yaml
skog:
  # The AWS profile skog works with. It is written here when you select one on the Profiles
  # page, so it is restored on the next start.
  profile: minio-local

  api:
    timeout: 30 # AWS API call timeout in seconds

  s3:
    # Entries one request asks for, and so the rows an objects page loads at a time; <n> asks
    # for another batch. The S3 API caps a response at 1000 however much is asked for.
    max_requested_entries: 1000
    # Cap on the keys <i> walks when aggregating a folder or a bucket. An aggregate that hits
    # it is marked with ">". Set to 0 for no cap — <Esc> is then what ends a long walk.
    max_scanned_keys: 100000

  download:
    # Where <d> writes what it fetches: <dir>/<bucket>/<key>. "~" and a relative path resolve
    # against your home directory, environment variables are expanded, and a file already
    # there is overwritten.
    dir: "~/Downloads/skog"

  viewer:
    # Largest object the viewer will open. The whole file is read, so anything over this is
    # refused rather than started on — <d> fetches those.
    max_object_size_mb: 256
    # Rows one batch of the viewer holds, and so how many <n> adds.
    page_rows: 500

  cache:
    # Where the viewer keeps object bodies, so opening one again costs no transfer.
    dir: "~/.cache/skog"
    # What the cache may hold. Over it, the object viewed longest ago is evicted first.
    # Set to 0 for no cap.
    max_size_mb: 2048

  # How much skog may change what a profile points at, keyed by profile name. <Tab> on the
  # Profiles page writes it. A profile not listed is regular, so this holds only what differs.
  modes:
    prod: read-only
    minio-local: yolo

  # A style file overriding the built-in colors; see examples/style.
  style: "~/.config/skog/uranium_v3.yaml"
```

A fuller commented example is in [`examples/config.yaml`](examples/config.yaml).

#### Configuration notes

**Defaults and merging**

- skog has built-in defaults for every section, embedded in the binary
- your `config.yaml` is merged on top: only the keys you write override a default, and every key you leave out keeps it
- sections merge key by key, so overriding `viewer.page_rows` leaves `viewer.max_object_size_mb` at its default
- whatever you set is applied, including `0` and `""`, but a non-positive number where one makes no sense — a zero timeout, an empty batch — falls back to the default with a warning in the log
- style files follow the same rules on top of the built-in style

**When the file is written**

skog writes `config.yaml` when you select a profile or switch a mode, and only then. It is
written from the configuration skog is running, the built-in defaults merged in, so **comments
and hand-formatting do not survive that write**.

**Environment**

- `SKOG_CONFIG_DIR` moves the config directory: `$SKOG_CONFIG_DIR/.config/skog/config.yaml`
- `${VAR}` in the config file is expanded when it is read
- `AWS_ENDPOINT_URL_S3` and `AWS_ENDPOINT_URL` are honoured for a profile that names no endpoint of its own, the service-specific one first, as in the AWS SDKs

**Where things go**

| Path | What |
|------|------|
| `~/.config/skog/config.yaml` | skog's own settings |
| `~/.config/skog/skog.log` | the log |
| `~/.cache/skog` | object bodies the viewer fetched (`cache.dir`) |
| `~/Downloads/skog` | what `d` wrote (`download.dir`) |
| `~/.aws/config`, `~/.aws/credentials` | your profiles — read, never written |

### Modes

A mode is a property of a profile rather than of the session, so it is set once per profile and
follows it:

| Mode | Reading | Modifying |
|------|---------|-----------|
| `read-only` | allowed | refused, with the reason on the status line |
| `regular` | allowed | asks first — the default |
| `yolo` | allowed | runs with no question |

`Tab` on the Profiles page cycles the highlighted profile through them and saves the choice. The
badge on the content border of every page says which mode the selected profile is in.

> [!WARNING]
> **`yolo` is not safe on a bucket you care about.** Deleting an object goes straight to S3 the
> moment `x` lands — no question, no undo. Deleting a folder takes every key under it the same
> way. A cursor on the wrong row is enough.
>
> Reading is never gated: `read-only` costs you nothing but the ability to delete. Pin the
> profiles that point at production to it, and leave `yolo` for a local MinIO you can rebuild.

Downloading is not a modification of what the profile points at — it reads S3 and writes your
disk — so it works in every mode, `read-only` included.

### Style

`skog.style` names a style file. A `~/` path is expanded; anything else is taken as written:

```yaml
skog:
  style: "~/.config/skog/uranium_v3.yaml"
```

The file is merged on top of the built-in style, so it needs to name only the colors it changes.
Colors are tcell names (`"white"`, `"grey"`) or hex (`"#1E1E1E"`); `"default"` inherits the
terminal's own.

**Ready-made themes** — see [`examples/style/README.md`](examples/style/README.md) for previews.

## Development

### Prerequisites

- Go 1.25 or newer
- something to point it at: an AWS account, or a local MinIO

### Building

```bash
git clone https://github.com/uraniumdawn/skog.git
cd skog

go mod download
go build -o skog
./skog
```

```bash
go test ./...
```

### A bucket to develop against

MinIO is the quickest thing to point skog at:

```bash
docker run -d -p 9000:9000 -p 9001:9001 \
  -e MINIO_ROOT_USER=minioadmin -e MINIO_ROOT_PASSWORD=minioadmin \
  minio/minio server /data --console-address ":9001"
```

```ini
# ~/.aws/config
[profile minio-local]
region = us-east-1
endpoint_url = http://localhost:9000

# ~/.aws/credentials
[minio-local]
aws_access_key_id = minioadmin
aws_secret_access_key = minioadmin
```

`SKOG_CONFIG_DIR=$PWD/.local ./skog` keeps a development config out of your real one.

### Logging

Everything goes to `~/.config/skog/skog.log`, one line per entry: an RFC3339 timestamp and the
caller as `file:line`. `INFO` covers startup and shutdown, `DEBUG` the event handler lifecycle,
`ERROR` failed operations and API errors. Since the TUI owns the terminal, a second one is the
usual way to read it:

```bash
tail -f ~/.config/skog/skog.log
grep ERROR ~/.config/skog/skog.log
```

## License

MIT — see [LICENSE](LICENSE).

## Acknowledgments

skog is built on:

- [tview](https://github.com/rivo/tview) — the terminal UI framework, and [tcell](https://github.com/gdamore/tcell) underneath it
- [aws-sdk-go-v2](https://github.com/aws/aws-sdk-go-v2) — the S3 client and the credentials behind it
- [arrow-go](https://github.com/apache/arrow-go) — the Parquet reader
- [hamba/avro](https://github.com/hamba/avro) — the Avro reader
- [go-cache](https://github.com/patrickmn/go-cache) — the page cache
- [fuzzy](https://github.com/sahilm/fuzzy) — inline search
- [zerolog](https://github.com/rs/zerolog) — logging

Borrowed ideas from [k9s](https://github.com/derailed/k9s) and [lazydocker](https://github.com/jesseduffield/lazydocker), and from the file managers the `h`/`l` navigation comes from — [ranger](https://github.com/ranger/ranger), [lf](https://github.com/gokcehan/lf) and [yazi](https://github.com/sxyazi/yazi).

## Support

Bugs and feature requests go to the [issue tracker](https://github.com/uraniumdawn/skog/issues).
For anything else, sirozhaua@gmail.com.

---
