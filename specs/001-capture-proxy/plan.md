# Implementation Plan: Comprehensive Protocol Capture Proxy

**Branch**: `001-capture-proxy` | **Date**: 2026-08-13 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/001-capture-proxy/spec.md`

## Summary

Build `sc-capture` — a Go module producing a single binary, `sccap` — that
archives the complete network footprint of a Star Conflict play session as a self-describing,
verifiable bundle, and knows enough about what it captured to report what has never been seen.

The architectural decision that shapes everything else: **capture happens at the wire with
the wire, not inside a proxy.** A MITM proxy terminates TCP and therefore cannot record the
real connection's transport framing (FR-004), cannot be byte-identical to an independent packet
capture (SC-002), and necessarily places a decoder in the byte path (Principle II). Capturing
frames off the interface satisfies all three by construction, and has the further effect that
in-match UDP traffic is archived whether or not a relayed match connection would be accepted —
removing the spec's single unproven assumption from the critical path.

Decoding, coverage tracking and annotation are therefore **downstream consumers of an
already-durable journal**, not participants in it. A decoder that panics, desyncs or meets an
unknown opcode cannot lose a byte, because the byte was written before the decoder ever saw it.
The same code path serves live decode and offline re-decode years later, which makes FR-029 and
FR-030 properties of the architecture rather than features to be built.

Interposition is retained as an opt-in overlay for the cases it uniquely serves, and the
dedicated-server relay is scoped to a time-boxed feasibility spike.

## Technical Context

**Language/Version**: Go 1.26 (toolchain present: go1.26.5; server repo targets 1.26.1)

**Primary Dependencies**: `github.com/gopacket/gopacket` v1.7.1 (`pcap`, `pcapgo`,
`reassembly`) — the only external dependency. Protocol framing, message typing and value
encodings are implemented in-repo in `pkg/scproto` (Principle VI as amended). No cgo —
`CGO_ENABLED=0`. No external module required: it builds from a fresh clone of this repository.

**Storage**: Filesystem bundles. pcapng segments (raw journal) + `index.jsonl` (derived record
index) + `session.json` (metadata) + `markers.log` + `SHA256SUMS`. Cross-session coverage state
in a single atomically-replaced JSON document under `%LOCALAPPDATA%\sccap\`.

**Testing**: `go test` with table-driven and golden-corpus tests; mandatory fault-injection suite
(decoder panic, malformed frame, stream desync, unknown message type) asserting byte-identical
journals; loopback end-to-end test against a synthetic server; cross-repo framing conformance
vectors.

**Target Platform**: Windows 10/11, x86-64 and arm64 (constitution v3.0.0). Live capture requires
[Npcap](https://npcap.com) and a `-tags npcap` cgo build; the plain build is a static binary with
no prerequisites that reads archives but cannot record. Capture also requires an elevated
process — Npcap refuses handles to unprivileged callers. Installing Npcap and elevating are host
setup and out of scope per the constitution — `sccap doctor` detects and reports it.

**Project Type**: CLI tool with on-disk format contracts (single binary, subcommands).

**Language policy**: Go only, with no second language anywhere in the build, tests or shipped
artifacts, and **no scripts in any language** (constitution v2.1.0). The install is one binary
with no runtime prerequisites. Two consequences the plan carries: the marker beacon and bundle
verification are `sccap` subcommands rather than the Python helpers that preceded them, and host
setup is **diagnosed, never orchestrated**. Third-party tools used as independent verification
oracles — a separate packet capture to diff against — are deliberately outside this rule; their
independence is the point.

**Licence**: MIT. The element tables are public domain (Unlicense) and impose nothing; the
copyleft constraint disappeared with the EUPL dependency. MIT is chosen so an unknown future
maintainer can pick this up with no legal analysis, which is the same reason the archive format
is standard — the artifact should outlive the project and its authors.

**Performance Goals**: Zero kernel drops at expected game traffic rates (< 10 Mbit/s, low
thousands of pps) — asserted from the capture backend's own `Stats()`, not assumed. Relay mode, when enabled,
adds ≤ 15 ms round-trip at p99 (SC-006). Live decode must never apply backpressure to capture.

**Scale/Scope**: One contributor, one machine, one session at a time. Sessions of ~30 min and up
to a few GB. Known protocol element universe ≈ 404 (39 message types, 249 `AC_*`, 116 `SN_*`).

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| # | Principle | Gate | Pre-Phase 0 | Post-Phase 1 |
|---|---|---|---|---|
| I | Capture Everything, Decide Later | No capture-time filter by default; any BPF filter is opt-in, recorded in `session.json`, and warned about. Unparseable/unknown traffic is journaled. | PASS | PASS — capture is filterless; scoping is by network namespace (isolation, not filtering) |
| II | Raw Bytes Are Evidence | No decoder anywhere in the write path. Decode reads from the durable journal only. | PASS | PASS — enforced structurally by R2; fault-injection suite asserts it |
| III | Observation Is Deadline-Bound | Never-observed set is persistent, cross-session, first-class, reportable on demand. | PASS | PASS — `coverage.json` + `sccap coverage` |
| IV | Do No Harm | Passive is the **default** mode. Every rewrite enumerated and recorded. No fuzz/replay/inject. Relay fully disableable and never required. | PASS | PASS — strengthened: capture no longer depends on the relay at all |
| V | Self-Describing and Verifiable | Schema version, software version, client build, UTC range, clock discipline, interposed connections, integrity hashes. Per-record wall + monotonic timestamps. | PASS | PASS — `session.json` + clock anchors (R4) + `SHA256SUMS` |
| VI | One Protocol Implementation | Exactly one implementation of framing, message typing and value encodings, in a package with no capture/storage/transport dependencies. | PASS | PASS — `pkg/scproto`; enforced by an import test and a no-second-parser review check |
| VII | Independently Useful Increments | Every increment produces a valid archived session on its own. | PASS | PASS — Increment 1 alone delivers US1 end to end |
| VIII | Contributor Safety | Sensitive by default and visible; no egress without explicit action; no primary account required. | PASS | PASS — owner-only DACL with inheritance severed, no network egress code paths exist at all |

**Development-workflow gates**: evidence before implementation (no decoder for an unobserved
shape) — honoured by treating `docs/protocol/` as a hypothesis to verify, never as ground truth;
feasibility spikes precede design — the UDP relay is scoped as a spike (R8); fault injection is a
required test — first-class in the test plan; every change states its archive effect — enforced
by the repository's pull request template.

**Result: no violations.** Complexity Tracking is empty.

### Design decisions that shift the spec's emphasis

Two places where this plan reads the spec differently than its wording suggests. Both are
deliberate, both are strictly stronger against the constitution, and neither drops a requirement.

1. **FR-008 (“interpose on every service”)** — interposition remains available, but capture is
   wire-level and does not depend on it. The spec was written proxy-first; the constitution's
   FR-004 and Principle II make a proxy an unsound place to keep evidence. All FR-008 services
   are still *observed*; they are simply observed rather than relayed by default.
2. **User Story 2 (in-match traffic)** — the spec makes relay feasibility the gate on archiving
   in-match traffic. Under this plan passive capture archives it regardless, and the relay
   question is demoted to a spike. US2's acceptance scenarios 1, 2 and 4 are satisfied
   passively; scenario 3 (relay refused) becomes the spike's expected-and-acceptable outcome.

## Project Structure

### Documentation (this feature)

```text
specs/001-capture-proxy/
├── plan.md              # This file
├── spec.md              # Feature specification (clarified 2026-08-13)
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/           # Phase 1 output
│   ├── cli.md               # `sccap` command surface
│   ├── session-bundle.md    # On-disk bundle layout + versioning
│   ├── session.schema.json  # `session.json` metadata schema
│   └── index.schema.json    # `index.jsonl` record schema
└── tasks.md             # Phase 2 output (/speckit-tasks — NOT created by /speckit-plan)
```

### Source Code (repository root)

A self-contained Go module inside this repository, per the constitution's settled commitment
that capture tooling is independently buildable:

```text
star-conflict-clone/                    # the repository
├── .github/workflows/ci.yml            # runs from the root, over sc-capture/
├── docs/
│   ├── Star-Conflict-Capture-Protocol.md
│   └── protocol/                       # frozen public-domain element universe (404) + sources
├── packet-caps/                        # submitted capture bundles
└── sc-capture/                         # Go capture tooling — self-contained module
    ├── go.mod                          # single external dep: gopacket
    ├── LICENSE.txt                     # MIT
    ├── README.md
    ├── cmd/
    │   └── sccap/
    │       └── main.go                 # subcommand dispatch only
    ├── pkg/
    │   └── scproto/                    # THE protocol implementation (Principle VI)
    │       ├── framing.go              # 12-byte BE header, incremental parse from a byte source
    │       ├── checksum.go             # MurmurHash2, seed 0x1337533d, LE header rendering
    │       ├── types.go                # message types, AC_* and SN_* ids
    │       ├── variant.go              # variant map ("bag") encoding
    │       ├── bitreader.go            # MSB-first bit reader for bit-packed bodies
    │       └── tables/                 # go:embed of docs/protocol/*.json (404 elements)
    ├── internal/
    │   ├── capture/                    # Npcap source, buffer config, drop accounting
    │   ├── journal/                    # pcapng writer, segmentation, flush/fsync, SHA256SUMS
    │   ├── session/                    # lifecycle, session.json, clock anchors, disk floor
    │   ├── flow/                        # flow table, service classification, handoff tracking
    │   ├── decode/                     # downstream consumer: reassembly → framing → decode
    │   │   └── isolate/                # panic containment per record
    │   ├── index/                      # index.jsonl writer + --rebuild
    │   ├── coverage/                   # never-observed set, atomic persistence
    │   ├── annotate/                   # markers.log, SCMARK beacon, derived annotations
    │   ├── verify/                     # integrity + completeness checks
    │   ├── doctor/                     # host diagnosis: caps, interfaces, offloads, clock, disk
    │   ├── relay/                      # OPTIONAL overlay: lb/shard/chat rewrites, udp spike
    │   └── status/                     # stderr status line
    ├── testdata/
    │   ├── golden/                     # framing vectors; checksum parity vs. the Python reference
    │   ├── corpus/                     # malformed frames, desync, unknown opcodes
    │   └── bundles/                    # small reference bundles for verify tests
    └── tests/
        ├── faultinjection/             # constitution-mandated
        ├── e2e/                        # loopback capture against synthetic server
        └── architecture/               # import rules: journal ⊅ decode, scproto ⊅ everything
```

**Structure Decision**: A self-contained `sc-capture/` Go module inside this repository.
`pkg/scproto` is the one exported package — it is the single protocol implementation Principle VI
requires, kept free of capture, storage and transport dependencies so a future server
reimplementation can consume it unchanged. Everything else is `internal/`, because nothing else
here is reusable.

Two import rules are the architecture's load-bearing invariants, both enforced by tests in
`tests/architecture`:

- `internal/journal` must **not** import `internal/decode` — no decoder in the byte path
  (Principle II).
- `pkg/scproto` must import nothing from `internal/` and nothing outside the standard library —
  keeps it consumable, and keeps a second parser from creeping in behind a convenience wrapper
  (Principle VI).

## Delivery increments

Ordered so each lands usable on its own (Principle VII). Increment 1 alone produces valid
archived sessions; if the servers shut down mid-development, everything after it is optional.

| # | Increment | Delivers | Spec coverage |
|---|---|---|---|
| 1 | Passive capture → verified bundle | `sccap capture`, `sccap verify`, `sccap doctor` | US1, US6; FR-001..007, 013, 025..028, 031..037 |
| 2 | Flow classification + handoff tracking | Services named in `session.json`; UDP match flow tagged | US2 (passive); FR-011, 012 |
| 3 | Decode + index + annotations | `index.jsonl`, `sccap mark`, derived annotations | US4; FR-015..019 |
| 4 | Coverage | `sccap coverage`, novel-element flagging | US3; FR-020..024 |
| 5 | Offline re-decode | `sccap index --rebuild`, `sccap decode` | US5; FR-029, 030 |
| 6 | Relay overlay + UDP spike | `sccap capture --relay`, feasibility verdict | US2 (relay); FR-008..010, 014 |

Increment 1 absorbs the two capabilities that used to live in external helpers — marker stamping
(was `sc-marker.py`) and bundle verification (was `verify_capture.py`) — because a contributor
capturing on day one needs both, and because a session that cannot be marked or verified is worth
materially less at intake. Both are better inside `sccap` than outside it: the beacon can stamp
the session's own clock anchors, and verification can read the session's own recorded drop
counters rather than inferring from a log.

## Risks

**Risk 1 — `pkg/scproto` is now written here rather than imported (see R6).** Framing, the
MurmurHash2 checksum, the variant-map encoding and the MSB-first bit reader are all implemented
in this repo. That is real work that was previously free, and a wrong checksum or a
byte-order slip is the kind of defect that stays invisible until it corrupts an interpretation.
*Mitigation*: none of it is guesswork — two independent reference implementations were read
before the sibling repos were removed, they agreed, and the Python one is archived verbatim at
`docs/protocol/source/protocol.py`. Golden vectors are generated from it and checked in, so
parity is a test failure rather than a mystery. The specific trap to encode as a test: the header
is big-endian on the wire but is fed to the checksum in **little-endian** order.

**Risk 2 — Npcap and elevation friction for non-expert contributors.** SC-001 gives a novice 15
minutes to a verified session; a capability-permission failure can consume all of it.
*Mitigation*: `sccap doctor` is the documented first command, diagnoses the exact missing
capability, and prints the precise command to fix it — mirroring how the manual handles the
capture-permission trap, which it calls the single most common setup failure.

**Risk 3 — Loopback traffic when the relay is enabled.** With `--relay`, client↔proxy traffic is
on `lo` while proxy↔server is on the NIC, so capture must bind both interfaces and the journal
contains both legs. *Mitigation*: multiple IDBs in the pcapng, each tagged in `session.json`;
`verify` asserts both legs are present when relay mode was active.

**Risk 4 — Capturing on the wrong interface produces a clean-looking, worthless session.** This
is the only failure mode that is both silent and total: zero drops, valid bundle, verification
passes, and none of the game's traffic in it. It is now a constitutional obligation to catch it
(v2.1.0: diagnose rather than orchestrate), and it is the strongest argument for detection over
the namespace scripts that used to handle it by construction. *Mitigation*: `sccap doctor
--watch` samples live traffic and reports which interfaces actually carry flows to game
endpoints; `sccap capture` warns within the first seconds if the selected interface is carrying
no game traffic while another one is, and records the interface inventory in `session.json` so
the mistake is at least attributable after the fact.

**Risk 5 — In-match UDP volume.** Realtime traffic at 30–60 Hz for a full match is the largest
data producer and the one with no prior measurement. *Mitigation*: the disk floor logic (R10) is
in Increment 1, ahead of the first match capture, not retrofitted after one fills a disk.

## Complexity Tracking

No constitution violations. Table intentionally empty.
