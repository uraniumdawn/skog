# Changelog

All notable changes to this project will be documented in this file.

---

## [0.3.0] - 2026-08-10

## Features

* **Topics as a YAML document in your editor** (`n`, `e` on the Topics page): `name`, `replication_factor`, `partitions` and a `configs` block. Settings at their cluster default come along as commented-out lines, so overriding one means uncommenting it. Parsed strictly — bad YAML, an unknown key, a renamed topic or a decreased partition count is reported with its line number and nothing is applied. Saving opens a confirmation page listing the exact changes; nothing hits the cluster until `Ctrl+Enter`.
* **Per-partition consumer group offsets in your editor** (`o` on the Consumer Group description): the group's committed offsets as YAML, one value per partition — the granularity the Reset Offsets modal can't reach. A value is an absolute offset, `earliest`/`latest`, `@<timestamp>` (formatted, RFC3339 or unix ms), or `none` to skip; one value on a topic covers all its partitions, and top-level `all:` covers every topic. Confirmation page, then a single `AlterConsumerGroupOffsets` call. Deleting a partition leaves it untouched.
* **Seed offsets for a topic a group has never consumed**: name an unlisted topic in the document, or use the modal's `+ new topic` row — partitions come from cluster metadata, and show as `(none) -> <offset>`. This writes a starting position only; nothing consumes the topic until a member subscribing to it joins.
* **Reset a topic config to the cluster default**: deleting a config line — or clearing its value — now sends an `IncrementalAlterConfigs` DELETE instead of being silently ignored. Works from the Edit Topic form too, where `key=""` keeps a genuinely empty value.

## Enhancements

* **The Create/Edit Topic and Reset Offsets form modals are now legacy** (`karat.features.create_edit_topic_modal`, `karat.features.reset_offsets_modal`, default `false`): `n`, `e` and `o` open a document instead. Set a flag to `true` to get the old form back on the same key. The forms may be removed in a future release. `Ctrl+O` inside a form is gone; the consume-parameters and connector-config editors are unchanged.
* **Shorter modifier notation in the keybinding bar**: `Ctrl+` is rendered as `C-` (`<C-u>`, `<C-Enter>`), the vim/kubectl convention.
* **Configurable editor** (`karat.editor` in `config.yaml`, default `vim`): used by every editor-backed view, split on whitespace so flags come along (`"code --wait"`).
* **The legacy Reset Offsets modal now shares the document's pipeline**: same resolve → range-check → confirm → commit path, so it gained the confirmation page, refused out-of-range targets and an accurate success count. A refused target leaves the modal open with every strategy still entered.

## Fixes

* **The Reset Offsets modal's "+ new topic" row works now**: partitions were taken solely from the group's committed offsets, so a new topic resolved to none — failing outright, or being silently dropped while the status line reported success. The name is now validated against cluster metadata.
* **The reset success message counted the wrong topics**: resetting one topic out of five reported "[5 topics]". It now reports the partitions actually committed.
* **A failed editor is reported instead of swallowed**: a missing binary surfaced later as "document is empty". It now reports `editor 'vim': executable file not found in $PATH — set karat.editor`. A non-zero exit (`:cq`) is treated as a deliberate abort — `nothing applied` — rather than applying whatever the file held.
* **A GUI editor without a wait flag is detected**: `editor: "code"` returned immediately and reported "no changes" with the tab still open. VS Code, Sublime Text and TextMate invoked without `--wait`/`-w` are now recognised and the flag is named.
* **The Reset Offsets modal's bottom bar tells the truth about `<Enter>`**: it always read `Select strategy`, but on the `+ new topic` row `<Enter>` starts typing a name. The menu now follows the selected row.
* **Out-of-range offsets are refused**: the broker accepts them and the consumer then silently overrides via `auto.offset.reset`, so a mistyped offset used to vanish without a trace. It is now reported with the bounds it had to fall between.

---

## [0.2.9] - 2026-08-09

## Features

* **Consume with one key** (`c` on the Topics list): starts consuming the selected topic immediately, with the parameters last used on it — no modal, no retyping. A topic that has never been consumed on the cluster falls back to the built-in defaults (`-o 100 [-r <sr>] -f '{…}'`). The parameters that ran are echoed in the status bar; a parse or schema-registry error is reported as usual and nothing starts.
* **Consume parameters history**: every parameter string that actually started a consume is remembered per cluster and topic in `~/.config/karat/history.yaml` (`KARAT_CONFIG_DIR` honoured), newest first, capped at 30 entries and de-duplicated. `Ctrl+R` in the Consume parameters modal opens a picker — the current topic's entries first, then the rest of the cluster's — where `Enter` fills the parameters in and `Ctrl+Enter` runs them straight away. The modal itself now opens prefilled with the topic's last-used parameters instead of the defaults. A missing or malformed history file is not an error: it simply starts empty.

## Enhancements

* **Consumer groups by topic from the Topics pages**: `.` → "Consumer groups" on the Topics list and the Topic description now opens the consumer groups that read the selected topic. Same page as the Consumer Groups list's "Find by topic", with the full consumer-groups functionality behind it (describe, delete, sort, search, `Ctrl+U` refresh, clone), without having to retype the topic name into a modal.
* **Edit consume parameters in your editor** (`Ctrl+O` on the Consume parameters modal): hands the current parameters to the editor, with the full flag reference — the same one `F1` shows — inlined below them as `#` comments, so the flags stay at hand while editing. Comment lines are dropped when the editor exits and the edited parameters go back into the modal. Uses the same suspend-the-TUI mechanism as the connector config editor.

---

## [0.2.8] - 2026-07-25

## Features

* **Topic size column**: the Topics list now shows each topic's actual on-disk size (summed across all replicas of all partitions). Sizes are fetched with a single `DescribeLogDirs` request per broker (via franz-go), filled in asynchronously so the list renders immediately, cached for 5 minutes, and refreshable with `Ctrl+U`. While the sizes are still on their way the header reads `Size…`. Sort by size with `3`. Requires franz-go connectivity — the column shows `-` when unavailable, and prefixes `~` when some replicas did not report (possible undercount).
* **Connector health column**: the Connectors list now shows a derived health per connector — `HEALTHY`, `DEGRADED` (some tasks failed), `UNHEALTHY` (connector failed, or running with no tasks / all tasks failed), plus `PAUSED` / `STOPPED` / `RESTARTING` / `UNASSIGNED` / `UNKNOWN`. Reuses the already-fetched connector statuses (no extra API calls). Sort by health with `4`.
* **Consumer-group lag column**: the Consumer Groups list (and the find-by-topic view) now shows each group's total lag — the sum over its committed partitions of (log-end − committed) offsets. Fetched with one `ListConsumerGroupOffsets` per group plus a single `ListOffsets` for all partitions, filled in asynchronously, cached for 5 minutes, and refreshable with `Ctrl+U`. While the lags are still on their way the header reads `Lag…`. Sort by lag with `3`.

## Enhancements

* **Configurable features** (`karat.features` in `config.yaml`): the columns that cost extra Kafka API calls can now be switched off individually — `topic_size` (Topics `Size` column and the topic description's actual size) and `consumer_group_lag` (Consumer Groups `Lag` column). Both default to `true`; setting one to `false` hides the column, drops its `3` sort key, and stops the extra cluster requests it needs. Useful on large or slow clusters.
* **Clone Subject** now copies the subject's entire schema version history — every version is fetched and re-registered oldest-first — instead of only the latest schema, preserving the full version lineage under the new subject.
* **Clone Consumer Group** (`.` → "Clone consumer group" on the Consumer Groups list): copies the source group's committed offsets to a new consumer group. Previously triggered by the `c` key on the Consumer Group description page; now consolidated into the Extra Actions modal to align with Clone Topic and Clone Subject.

---

## [0.2.7] - 2026-06-21

## Features

* **Clone Topic** (`.` → "Clone topic" on Topics list or Topic description): fetches the source topic's partition count, replication factor, and non-default config entries, then opens a pre-filled modal to create a new topic with the same configuration.
* **Clone Subject** (`.` → "Clone subject" on Subjects list): fetches the source subject's latest schema and compatibility level, then opens a modal to register the schema under a new subject name with the same compatibility.
* **Delete Subject** (`.` → "Delete subject" on Subjects list): soft-deletes all versions of a subject, with a confirmation modal.
* **Delete Subject Version** (`.` → "Delete version" on Versions page): soft-deletes a specific version of a subject, with a confirmation modal.
* **Find Schema by ID** (`.` → "Find schema by ID" on Subjects list): enter a global schema ID to view the schema, with JSON (`2`) / Karat (`1`) format toggle.
* **Extra Actions for Subjects and Versions**: `.` key now opens the Extra Actions modal on the Subjects list and Versions pages.
* **Extra Actions for Consumer Groups**: `.` key opens the Extra Actions modal on the Consumer Groups page, starting with "Find by topic".

## Enhancements

* Schema description page title now shows `<subject> [v:<version>] [id:<schema_id>]`.
* Karat format is now the default view for schema descriptions (both by subject/version and by ID), falling back to JSON when Karat formatting fails. Toggle with `1` (karat) / `2` (JSON).
* Extra Actions keybinding changed from `>` to `.` across all pages (Topics, Consumer Groups, Subjects, Versions).
* "Find consumer group by topic" moved from the `f` keybinding into the Extra Actions modal and simplified to a single input field with `Ctrl+Enter` to submit.
* Read-only mode is now enforced for Schema Registry mutating operations (clone, delete) — blocked with a status message when the registry is marked read-only.

## Bug Fixes

* Fixed swapped `Partitions` / `ReplicationFactor` arguments in `CreateTopicResultHandler` call.

---

## [0.2.6] - 2026-06-14

## Enhancements

* Borders now omit vertical line characters, giving panels a cleaner, more minimal look (back to how it looks in 0.2.4).

---

## [0.2.5] - 2026-06-14

## Features

* **Karat format for Avro schemas**: on the Schema Registry "Schema" description page, press `1` to view the schema in Karat's compact, hierarchical text format (`<name>:<type>:<extra>` per line), or `2` to switch back to pretty-printed JSON (default).

## Enhancements

* Stopping consumption (`t`) now reacts immediately, even on hot topics — the consumer goroutine and UI no longer block on a backlog of in-flight messages/draws.

---

## [0.2.4] - 2026-06-13

## Features

* **Transactions page**: browse cluster-wide Kafka transactions (transactional ID, producer ID, state, timeout, partitions), with sorting (`1`/`2`) and search (`/`). Requires franz-go connectivity for the cluster.
* **ACLs page**: view cluster ACLs (principal, resource type/name, pattern type, operation, permission), with sorting and search. Requires franz-go connectivity for the cluster.
* **Topic Producers**: view active producers and in-flight transactions for each partition of a topic, via the new "Extra Actions" modal.
* **Extra Actions modal** (`>` on the Topics list and the Topic description page): consolidates secondary actions — "Consume", "CLI commands", and "Producers".
* **Hide internal topics** (`i` on the Topics list): toggle hiding topics matching configurable regex patterns. Defaults (`^__.*`, `.*-changelog$`, `.*-repartition$`) can be overridden via `karat.ui.internal_topic_patterns` in the config file.
* **Connector Offsets**: new `o` keybinding on the Connector description page opens an offsets view; `Ctrl+d` deletes the connector's offsets (connector must be stopped first), and `c` copies offsets to another connector.
* Topic description page now shows the topic's actual on-disk size, aggregated across replicas.
* Connector actions: added `STOP` for running connectors and `RESUME` for stopped connectors.

## Enhancements

* Auto-update keybinding changed from `Ctrl+g` to `g`, and removed from list pages (Topics, Consumer Groups, Subjects, Versions, Connectors, Connector Details) — still available on detail/description pages.
* Auto-update intervals changed from 1s/3s/5s/10s/20s to 1s/5s/10s/30s/60s.
* Consumer groups: "Copy to..." keybinding changed from `Ctrl+e` to `c`.
* Connectors list loads faster — connector statuses are now fetched concurrently (up to 8 in parallel) instead of sequentially.

---

## [0.2.3] - 2026-06-02

## Features

* **Latest version check**: on startup karat checks GitHub for the latest release and notifies the user if a newer version is available.

## Bug Fixes

* Remove invalid `RESTART` action from the connector actions modal for `PAUSED` connectors — only `RESUME` is valid in that state.

---

## [0.2.2] - 2026-06-01

## Features

* **Copy Consumer Group offsets** (`Ctrl+e` on consumer group detail page): opens a modal to enter a target group name; on `Ctrl+Enter` copies the committed offsets of the source group to the new group, implicitly creating it.
* Add theme previews and style gallery to `examples/style/README.md`.

## Bug Fixes

* Embed `default_config.yaml` and `default_style.yaml` in the binary via `//go:embed` — fixes crash on startup when installed via Homebrew or run outside the source root.

---

## [0.2.1] - 2026-05-30

## Maintenance

* Add GitHub Actions release workflow (`.github/workflows/release.yml`) using GoReleaser.

---

## [0.2.0] - 2026-05-30

* Initial public release.

---

[Unreleased]: https://github.com/uraniumdawn/karat/compare/v0.2.9...HEAD
[0.2.9]: https://github.com/uraniumdawn/karat/compare/v0.2.8...v0.2.9
[0.2.8]: https://github.com/uraniumdawn/karat/compare/v0.2.7...v0.2.8
[0.2.7]: https://github.com/uraniumdawn/karat/compare/v0.2.6...v0.2.7
[0.2.6]: https://github.com/uraniumdawn/karat/compare/v0.2.5...v0.2.6
[0.2.5]: https://github.com/uraniumdawn/karat/compare/v0.2.4...v0.2.5
[0.2.4]: https://github.com/uraniumdawn/karat/compare/v0.2.3...v0.2.4
[0.2.3]: https://github.com/uraniumdawn/karat/compare/v0.2.2...v0.2.3
[0.2.2]: https://github.com/uraniumdawn/karat/compare/v0.2.1...v0.2.2
[0.2.1]: https://github.com/uraniumdawn/karat/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/uraniumdawn/karat/releases/tag/v0.2.0
