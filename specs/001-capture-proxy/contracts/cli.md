# Contract: `sccap` command surface

**Feature**: `001-capture-proxy` | **Status**: Design | **Schema**: `bundle/1.0`

The binary is the only interface this project exposes to contributors. Everything below is a
compatibility surface: flag names, exit codes and output shapes are part of the contract.

## Global conventions

- **One static Go binary, no runtime prerequisites, no scripts** (constitution v2.1.0). Anything a
  contributor runs is a subcommand here. `sccap mark` and `sccap verify` exist because the
  Python helpers that used to provide them (`sc-marker.py`, `verify_capture.py`) are not being
  recreated in any language.

- **stdout** carries data meant to be piped (reports, JSON). **stderr** carries progress,
  warnings and diagnostics. A contributor reading the screen and a script reading a pipe never
  contend.
- `--json` on any reporting subcommand emits machine-readable output on stdout instead of the
  human table.
- No subcommand contacts the network except `capture --relay`, which contacts only the game's
  own servers. There is no telemetry, no update check, and no submission path (FR-032, SC-009).
- Exit codes: `0` success · `1` usage error · `2` verification failed · `3` capability/permission
  missing · `4` disk floor reached · `5` schema version unreadable.

---

## `sccap doctor`

Checks whether this machine can capture, and says exactly what to fix if not. **The documented
first command** — SC-001's 15-minute budget dies on a permission failure discovered late.

```
sccap doctor [--interface <if>] [--watch <duration>] [--json]
```

Checks: a capture backend is compiled in (`-tags npcap`) and the process is elevated — reported
as two distinct checks, because they fail for unrelated reasons with unrelated remedies;
interface inventory, link state and routable-address carrier; NIC offload state; free disk
against the warn threshold; Windows Time service state; writability of the coverage directory;
identification of the installed game client; embedded protocol table revision.

Offload state is reported as **unknown with the commands to check it**, never guessed: LSO and
RSC live behind per-driver properties with no stable names across vendors, and a diagnostic that
sometimes lies about frame fidelity is worse than one that admits what it cannot see.

`--watch <duration>` samples live traffic and reports **which interfaces actually carry flows to
game endpoints**. This is the check that matters most: capturing on the wrong interface yields a
clean-looking session — zero drops, valid bundle, passing verification — containing none of the
game's traffic. It is the only failure mode that is both silent and total.

Each failed check prints the precise remediation command. `doctor` **never changes host state**:
host configuration is out of scope per the constitution, and no scripts ship in any language.
Detection is the obligation; configuration is the contributor's, with the exact command supplied.

Exit `3` if capture would be impossible.

---

## `sccap capture`

Records a session. This is the feature.

```
sccap capture [flags]

  --scenario <ID>        Scenario id for the bundle name (default: ADHOC)
  --volunteer <ID>       Volunteer id (default: vol-local)
  --region <R>           Server region: EU|NA|RU|SEA (default: unset)
  --interface <if>       Capture interface; repeatable (default: autodetect + lo)
  --out <dir>            Bundle parent directory (default: ./captures)
  --min-free <size>      Warning threshold (default: 2GiB)
  --floor <size>         Hard stop threshold (default: 512MiB)
  --relay                Enable interposition overlay (default: OFF — passive)
  --relay-udp            Include the dedicated-server relay spike (implies --relay)
  --snaplen <n>          Bytes per frame (default: 0 = full frame)
  --filter <bpf>         Capture-time BPF filter (default: none)
  --no-decode            Journal only; skip live decode entirely
```

**Defaults are the contract.** Passive, unfiltered, full-snaplen (Principles I and IV). Passing
`--filter` or a non-zero `--snaplen` prints a prominent warning that the session now contains a
capture-time discard decision, and records that fact in `session.json`.

Runs until SIGINT/SIGTERM, then closes the session cleanly. SIGKILL or power loss leaves an
`interrupted` session that is still valid and verifiable (FR-006, SC-008).

**Progress line** (stderr, refreshed 1 Hz):

```
[00:04:12] services=lb,shard,chat  frames=182401  journal=241MiB  drops=0  records=9188  novel=2
```

`drops` is on the line deliberately — it is the number that tells a contributor their capture is
worthless before they spend an hour on it. Novel elements additionally print one persistent line
each.

---

## `sccap mark`

Stamps a contributor annotation onto the running session's timeline (FR-018).

```
sccap mark "<label>"          # one-shot
sccap mark --console          # interactive: label + Enter, repeatedly
```

Writes the beacon line to `markers.log` **and** broadcasts the same ASCII datagram on the capture
interface so it lands inline in the pcapng, preserving the manual's three-way video↔packet↔log
binding:

```
SCMARK|000042|2026-08-14T20:31:40.118Z|884421993310|EVENT|purchase Mk3 Shield Booster
```

---

## `sccap verify`

Confirms a bundle is complete and internally consistent, before a contributor shares it (FR-028).

```
sccap verify <bundle-dir> [--json] [--write-sums]
```

Checks, in order: `session.json` parses and its schema MAJOR is readable; every file in
`SHA256SUMS` exists and hashes match; every pcapng segment walks structurally end to end; the
recorded `packets_dropped` is 0; `index.jsonl` (if present) references only frames that exist;
clock anchors are monotonic; the bundle's DACL grants no principal beyond the owner and SYSTEM.

The permissions check reports **which accounts hold access**, not a mode. A mode would be a lie
here — `os.Chmod` toggles the read-only attribute and nothing else, so a bundle reporting `0700`
could still be readable by every account on the machine.

An interrupted session **passes** with an explicit `interrupted` status, reporting the last
consistent point. Exit `2` on any hash mismatch or structural failure.

---

## `sccap index`

Builds or rebuilds the derived record index.

```
sccap index <bundle-dir> [--rebuild] [--decoder <version>]
```

`--rebuild` regenerates `index.jsonl` from the pcapng segments alone. This is the operation that
makes FR-030 true: an improved decoder is applied to an archive years later and the raw journal
is not touched. `verify` after a rebuild must still pass with identical pcapng hashes.

---

## `sccap decode`

Reads an archived session and prints its records. Works with every game service unreachable
(FR-029).

```
sccap decode <bundle-dir> [--conn <id>] [--type <name>] [--status <s>] [--json]
```

Filtering here is **read-time filtering against a complete archive**, which the constitution
permits explicitly — as distinct from capture-time filtering, which it forbids.

Records that cannot be decoded print as undecoded raw bytes with their frame references; they are
never omitted and never shown as a speculative partial interpretation (FR-017).

---

## `sccap coverage`

Reports what has never been observed (FR-021). The project's progress metric.

```
sccap coverage [--json] [--kind message_type|async_request|notification]
               [--state never_observed|observed_undecoded|decoded]
               [--ingest <bundle-dir>]
```

Default human output:

```
Element coverage (404 known)
  message_type      39 known    31 decoded    5 undecoded     3 never observed
  async_request    249 known    32 decoded   61 undecoded   156 never observed
  notification     116 known    19 decoded   24 undecoded    73 never observed

Never observed (232) — run `sccap coverage --state never_observed` for the full list
Novel elements seen but unknown to this build: 2
```

`--ingest` folds a bundle's `coverage-delta.json` into machine-wide state, so a contributor can
rebuild coverage from bundles alone.

---

## `sccap status`

Prints a snapshot of the running session, in the same shape as the progress line. Reads the
session's live state file; does not attach to or perturb the capture.

---

## Subcommands deliberately absent

| Not provided | Why |
|---|---|
| `sccap submit` / `upload` | Submission is out of scope (clarified 2026-08-13). No egress path exists in the binary at all — this is what makes SC-009 verifiable by inspection rather than by testing. |
| `sccap prune` / `gc` | The system must never delete or overwrite prior sessions to reclaim space (FR-036). Removal is the contributor's `rm`. |
| `sccap replay` / `fuzz` | Forbidden outright (FR-014, Principle IV). |
| `sccap edit` | Editing a captured session would destroy the evidence property. Derived data is rebuilt, never edited. |
| `sccap setup` | Host configuration is out of scope and reimplementing administrative work in Go is a downgrade. `doctor` detects and names every condition it would have configured. |
