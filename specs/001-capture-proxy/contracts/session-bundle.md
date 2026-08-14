# Contract: session bundle on-disk format

**Feature**: `001-capture-proxy` | **Schema**: `bundle/1.0`

A bundle is the unit of verification and sharing. This layout is a **superset** of the bundle
defined in `docs/Star-Conflict-Capture-Protocol.md` §2.1, so bundles produced by `sccap` and by
the manual's shell tooling are interchangeable at intake.

## Layout

```
SC_20260814T203015Z__AUTH-02__vol-042__EU__000/
├── capture_00001_20260814203015.pcapng    # EVIDENCE — raw journal, segmented
├── capture_00002_20260814204015.pcapng
├── session.json                           # REQUIRED — metadata, clock anchors, connections
├── index.jsonl                            # derived — one record per protocol unit
├── coverage-delta.json                    # derived — what this session contributed
├── markers.log                            # contributor + derived annotations
├── capture.log                            # AF_PACKET stats; proves captured/dropped counts
├── client_version.txt                     # game build identity
├── notes.md                               # optional, contributor free text
└── SHA256SUMS                             # written last, at clean close
```

Files from the manual's bundle that `sccap` does not produce but preserves if present:
`video.mkv`, `sockets.csv`, `sslkeys.log`, `steam-<appid>.log`, `display-filter.txt`.

## Naming

`SC_<UTC_START>__<SCENARIO_ID>__<VOLUNTEER_ID>__<REGION>__<SEQ>` — unchanged from §2.2 of the
manual. UTC always, ASCII only, no spaces or colons. The directory name is the session identity;
`session.json.bundle_id` repeats it and `verify` checks they agree.

## File roles and the evidence boundary

| File | Class | Regenerable | Loss consequence |
|---|---|---|---|
| `capture_*.pcapng` | **Evidence** | **No** | The archive is gone. Nothing else matters. |
| `session.json` | Evidence-adjacent | No (contains observations made only at capture time) | Session is unreadable without tacit knowledge — fails Principle V |
| `markers.log` | Evidence-adjacent | No (contributor testimony) | Timeline loses human context; capture still valid |
| `index.jsonl` | Derived | Yes — `sccap index --rebuild` | None |
| `coverage-delta.json` | Derived | Yes — re-ingest | None |
| `capture.log` | Derived | No, but non-essential | Cannot prove zero drops |
| `SHA256SUMS` | Derived | Yes — `verify --write-sums` | Integrity unprovable until regenerated |

The boundary is the contract: **a bundle with only pcapng segments and a valid `session.json` is
a valid session.** Everything else can be absent, truncated or rebuilt.

## pcapng conventions

- One Section Header Block per file. `shb_userappl` = `sccap <version>`; `shb_os` records the
  kernel and distribution.
- One Interface Description Block per captured interface. `if_tsresol = 9` (nanoseconds);
  `if_name` and `if_description` identify the interface, and the description states its role
  (`game-uplink`, `loopback-relay-leg`).
- Enhanced Packet Blocks only. No Simple Packet Blocks — they lack timestamps.
- Segment rotation at 200 MB or 10 minutes, whichever comes first, matching the manual's soft
  caps. The outgoing segment is flushed and fsynced before the next is opened.
- No packet is modified, reordered or omitted between wire and block. Snaplen defaults to the
  full frame.
- Frame numbering for `index.jsonl` references is **`(segment_file, frame_index_within_segment)`**
  — global numbering across segments is not used, because it cannot survive a lost segment.

## Versioning and compatibility

`session.json.schema_version` is `MAJOR.MINOR`, currently `1.0`, and governs the whole bundle.

- A reader encountering a **known MAJOR** must read the bundle, ignoring unknown fields.
- A reader encountering an **unknown MAJOR** must refuse with an explicit diagnostic naming both
  versions, and must never attempt a partial read (FR-027). Exit code `5`.
- MINOR is incremented for additive fields; MAJOR for any change to the meaning or removal of a
  field, or to the pcapng conventions above.
- The schema version is written before any capture begins, so an interrupted session still
  declares its version.

## Integrity

`SHA256SUMS` covers **every file in the bundle**, itself excluded, in the standard
`<hex>  <relative-path>` format (FR-026). It is written at clean close, after the final flush.

An interrupted session has no `SHA256SUMS`. `sccap verify` reports such a session as
`interrupted` rather than failed, hashes what is present, and `--write-sums` will generate the
file — recording in `session.json.anomalies` that sums were generated post hoc rather than at
close, so the distinction is never lost.

## Permissions

Directory `0700`, all files `0600`, set at creation (FR-031). `verify` treats looser permissions
as a warning, not a failure — the contributor may have deliberately relaxed them to share, and
refusing to verify a bundle they can no longer fix would be unhelpful.

## Sensitivity

Every bundle carries `session.json.sensitive = true` and a plain-language
`sensitivity_reason`. Bundles routinely contain authentication material: the master-server
protocol has no transport TLS, so `CCMD_AUTH_REQUEST` (type 4) and `AC_PLAYER_CREDENTIALS`
(opcode 9) traffic is in the clear in the journal. When either is observed,
`credential_warning` is set and the contributor is told at close, not buried in a file.
