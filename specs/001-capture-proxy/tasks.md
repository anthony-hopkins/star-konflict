---

description: "Task list for 001-capture-proxy"
---

# Tasks: Comprehensive Protocol Capture Proxy

**Input**: Design documents from `/specs/001-capture-proxy/`

**Prerequisites**: [plan.md](./plan.md), [spec.md](./spec.md), [research.md](./research.md), [data-model.md](./data-model.md), [contracts/](./contracts/)

**Tests**: Included, but **only where the constitution or spec mandates them** — this is not a blanket TDD suite. Specifically: the fault-injection suite (constitution, Development Workflow: "Fault injection is a required test"), golden protocol vectors (Principle VI verification), the architecture import rules (Principles II and VI), and the acceptance tests named in the spec's Independent Test sections. Everything else is left to judgement during implementation.

**Organization**: Grouped by user story. Phase order follows the plan's delivery increments rather than raw spec priority — see the note below.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (US1–US6)
- All paths are relative to the repository root unless prefixed `sc-capture/`

## Path Conventions

New self-contained Go repository at `sc-capture/`, per plan.md Structure Decision:

- `sc-capture/cmd/sccap/` — subcommand entry points
- `sc-capture/pkg/scproto/` — the single protocol implementation (Principle VI)
- `sc-capture/internal/` — everything else
- `sc-capture/tests/` — architecture, fault-injection and end-to-end suites

**No Makefile, no shell helpers, no task runner.** `go build` and `go test` are the interface (constitution v2.1.0: no scripts ship, in any language).

## Phase ordering note

Spec priority is US1/US2 (P1) → US3/US4 (P2) → US5/US6 (P3). Delivery order below differs deliberately, per Principle VII (every increment usable the day it lands):

- **US6 (safety) moves earlier** — it is not a feature to add later. Permissions, sensitivity marking and the absence of any egress path must be true of the very first byte written to disk.
- **US4 (decode) precedes US3 (coverage)** — coverage is computed from decoded records, so it cannot exist first.
- **US2 splits in two.** Passive in-match capture (Phase 6) needs no relay and carries the story's irreplaceable value. The relay feasibility spike (Phase 10) is time-boxed and may legitimately fail.

**`pkg/scproto` deliberately does not appear until Phase 6.** Raw capture needs no protocol knowledge — that is the whole point of the architecture — so the MVP ships without a line of protocol code.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Create the repository and its build

- [X] T001 Create `sc-capture/` directory tree per plan.md Structure Decision (`cmd/sccap/`, `pkg/scproto/tables/`, `internal/{capture,journal,session,flow,decode/isolate,index,coverage,annotate,verify,doctor,relay,status}/`, `testdata/{golden,corpus,bundles}/`, `tests/{architecture,faultinjection,e2e}/`)
- [X] T002 Initialize module in `sc-capture/go.mod` as `github.com/sc-re/sc-capture`, Go 1.26, and add the sole dependency `github.com/gopacket/gopacket v1.7.1`
- [X] T003 [P] Add MIT licence text to `sc-capture/LICENSE.txt` (research.md R15)
- [X] T004 [P] Write `sc-capture/README.md` covering build, `CGO_ENABLED=0`, capability grant, and the four completeness checks
- [X] T005 [P] Add CI in `sc-capture/.github/workflows/ci.yml`: `go vet`, `go test ./...`, and `CGO_ENABLED=0` cross-compile for linux/amd64 and linux/arm64
- [X] T006 [P] Add `sc-capture/.github/pull_request_template.md` requiring every change to declare its effect on the archive — what is captured, what is persisted, whether the on-disk schema changed (constitution, Development Workflow)
- [X] T007 [P] Copy the three element tables from `docs/protocol/` into `sc-capture/pkg/scproto/tables/` (`message-types.json`, `async-requests.json`, `notifications.json`) for `go:embed`

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The command shell, the clocks, and the host diagnosis that everything else needs

**⚠️ CRITICAL**: No user story work can begin until this phase is complete

- [X] T008 Implement subcommand dispatch in `sc-capture/cmd/sccap/main.go`, with the stdout/stderr split from contracts/cli.md (data on stdout, progress and diagnostics on stderr)
- [X] T009 [P] Define exit codes in `sc-capture/internal/exitcode/exitcode.go`: 0 success, 1 usage, 2 verification failed, 3 capability missing, 4 disk floor, 5 schema unreadable
- [X] T010 [P] Implement the clock source in `sc-capture/internal/session/clock.go`: paired `CLOCK_REALTIME`/`CLOCK_MONOTONIC` anchors at start, every 30s and at end, plus step detection when realtime diverges from monotonic by more than 1s (research.md R4)
- [X] T011 [P] Implement free-space monitoring in `sc-capture/internal/session/disk.go` with `--min-free` warn threshold and hard floor, leaving enough headroom to write `session.json` and `SHA256SUMS` (research.md R10)
- [X] T012 [P] Implement the 1 Hz stderr status line in `sc-capture/internal/status/status.go` — elapsed, services, frames, journal size, **drops**, records, novel (research.md R11)
- [X] T013 Implement host diagnosis in `sc-capture/internal/doctor/doctor.go`: `CAP_NET_RAW`/`CAP_NET_ADMIN`, interface inventory and carrier, NIC offload state, free disk, clock discipline, coverage dir writability, embedded table revision. Each failure prints the exact remedy; **never mutates host state**
- [X] T014 Implement `--watch` traffic sampling in `sc-capture/internal/doctor/watch.go`, reporting which interfaces actually carry flows to game endpoints (plan Risk 4 — the only silent, total failure mode)
- [X] T015 Wire `sccap doctor` in `sc-capture/cmd/sccap/doctor.go` per contracts/cli.md, exiting 3 when capture would be impossible
- [X] T016 [P] Add architecture import rules in `sc-capture/tests/architecture/imports_test.go`: `internal/journal` must not import `internal/decode` (Principle II); `pkg/scproto` must import nothing from `internal/` and nothing outside the standard library (Principle VI)

**Checkpoint**: `sccap doctor` runs and tells a contributor whether this machine can capture

---

## Phase 3: User Story 1 — Record a complete play session, byte-exact and verifiable (Priority: P1) 🎯 MVP

**Goal**: One command produces a self-contained, verifiable session containing every byte that crossed the wire, interpretable by someone who was not present.

**Independent Test**: Run the program, play a login-to-hangar session, stop it. Independently capture the same traffic with a general-purpose packet analyser and assert the two byte streams are identical. Confirm the session verifies itself with no external input.

### Implementation for User Story 1

- [X] T017 [US1] Implement the `AF_PACKET` v3 ring source in `sc-capture/internal/capture/afpacket.go` — no BPF filter and full snaplen by default; a non-default value for either is recorded and warned about (Principle I)
- [X] T018 [P] [US1] Implement kernel drop accounting in `sc-capture/internal/capture/stats.go`, exposing the counter that SC-002 and SC-003 assert on
- [X] T019 [US1] Implement the pcapng writer in `sc-capture/internal/journal/pcapng.go`: one SHB per file, one IDB per interface with `if_tsresol = 9` and a role description, EPB only (contracts/session-bundle.md)
- [X] T020 [US1] Implement segmentation and durability in `sc-capture/internal/journal/segment.go` — rotate at 200 MB or 10 min, flush and fsync every 1 s or 4 MB, fsync the outgoing segment before opening the next (research.md R9)
- [X] T021 [US1] Implement bundle creation in `sc-capture/internal/session/bundle.go` — `SC_<UTC>__<SCENARIO>__<VOLUNTEER>__<REGION>__<SEQ>` naming, directory `0700` and files `0600` **set at creation, not chmod'ed afterwards** (research.md R12, R13)
- [X] T022 [US1] Implement `session.json` in `sc-capture/internal/session/metadata.go` against `contracts/session.schema.json`: schema version written before capture begins, software and table versions, client, host, interfaces, clock anchors, termination state
- [X] T023 [P] [US1] Implement `SHA256SUMS` generation over every bundle file in `sc-capture/internal/journal/hashes.go`, written last at clean close
- [X] T024 [US1] Wire `sccap capture` in `sc-capture/cmd/sccap/capture.go` with the full flag set from contracts/cli.md, defaulting to passive, unfiltered, full-snaplen
- [X] T025 [US1] Handle SIGINT/SIGTERM for clean close in `sc-capture/cmd/sccap/capture.go`; confirm SIGKILL leaves `termination: interrupted` with no `utc_end` and a structurally walkable journal
- [X] T026 [US1] Integrate the disk floor into the capture loop — repeated warnings past the threshold, then clean close and exit 4 on reaching the floor, never deleting a prior session (FR-036, FR-037)
- [X] T027 [US1] Implement verification in `sc-capture/internal/verify/verify.go`: schema MAJOR readable first, hashes, structural pcapng walk, drops == 0, index frame references valid, clock anchors monotonic, permissions; an interrupted session **passes** with explicit status
- [X] T028 [US1] Wire `sccap verify` in `sc-capture/cmd/sccap/verify.go` with `--json` and `--write-sums`, exiting 2 on hash mismatch or structural failure
- [X] T029 [US1] Implement marker stamping in `sc-capture/internal/annotate/marker.go` — append the `SCMARK|seq|utc|mono|kind|label` line to `markers.log` **and** broadcast the same datagram so it lands inline in the pcapng (research.md R14, FR-018)
- [X] T030 [P] [US1] Wire `sccap mark` in `sc-capture/cmd/sccap/mark.go`, one-shot and `--console` modes

### Tests for User Story 1 (spec Independent Test + Success Criteria)

- [X] T031 [P] [US1] Byte-exactness test in `sc-capture/tests/e2e/byteexact_test.go` — capture alongside an independent tool and assert identical packet bytes, including transport headers and checksums (SC-002, FR-004)
- [X] T032 [P] [US1] Abrupt-termination test in `sc-capture/tests/e2e/abrupt_test.go` — SIGKILL mid-capture across 20 trials, asserting a valid, verifiable session in 100% (SC-008)
- [X] T033 [P] [US1] Disk-floor test in `sc-capture/tests/e2e/diskfloor_test.go` against a small tmpfs — clean close, `termination: disk_floor`, exit 4, prior sessions untouched

**Checkpoint**: 🎯 **MVP.** A contributor can archive a verified session. Everything after this is enrichment — if the servers shut down here, the tool was already collecting evidence.

---

## Phase 4: User Story 6 — Contribute without exposing yourself (Priority: P3, delivered with the MVP)

**Goal**: The contributor knows their session is sensitive, and nothing leaves their machine.

**Independent Test**: Complete a session while monitoring all network egress. Assert no captured data leaves the machine, and that the session is marked sensitive without the contributor having to know it should be.

**Why here**: safety is not a later feature. These properties must hold for the first byte written, so they land with the MVP rather than after it.

- [X] T034 [US6] Set `sensitive: true` unconditionally with a plain-language `sensitivity_reason` in `sc-capture/internal/session/metadata.go` (FR-031)
- [X] T035 [P] [US6] Permissions test in `sc-capture/tests/e2e/permissions_test.go` — directory `0700`, files `0600`, asserted at creation time rather than after the fact
- [X] T036 [P] [US6] Egress test in `sc-capture/tests/e2e/egress_test.go` — monitor all outbound traffic across a full session and assert nothing but game servers is contacted (SC-009)
- [X] T037 [P] [US6] Passive-default test in `sc-capture/tests/e2e/passive_test.go` — with no flags, assert `mode: passive` and an empty `rewrites` array (FR-013, Principle IV)

> Note: the credential-specific warning (FR-033) needs protocol decoding to detect `CCMD_AUTH_REQUEST` and `AC_PLAYER_CREDENTIALS`, and lands as T057 in Phase 7. Unconditional sensitivity marking above does not depend on it.

**Checkpoint**: US1 and US6 both hold. This is the increment worth shipping to contributors.

---

## Phase 5: User Story 2a — Reach the in-match traffic nothing has ever recorded (Priority: P1)

**Goal**: The realtime gameplay protocol is journaled in both directions, identified from the protocol rather than guessed. Passive — no relay, no rewriting, no ban exposure.

**Independent Test**: Enter a non-competitive match. Assert the handoff decodes and that UDP records appear in both directions with timestamps, with the connection's `service_evidence` reading `handoff`.

### Protocol implementation (Principle VI — the single implementation)

- [X] T038 [P] [US2] Embed and expose the element tables in `sc-capture/pkg/scproto/tables.go` — `go:embed` the three JSON files, lookup by kind and id, report table revision
- [X] T039 [P] [US2] Define message, async-request and notification types with names in `sc-capture/pkg/scproto/types.go`
- [X] T040 [US2] Implement the checksum in `sc-capture/pkg/scproto/checksum.go`: MurmurHash2, seed `0x1337533d`, `m = 0x5bd1e995`, `h` initialised to `12 ^ seed`, **header fed little-endian though the wire is big-endian** (research.md R6)
- [X] T041 [US2] Implement framing in `sc-capture/pkg/scproto/framing.go`: 12-byte big-endian header, incremental parse from a byte source (never a `net.Conn`), and the special frames — `body_len > 0xfffffc` is not a length, `ff ff ff ff` is a disconnect, `ff ff ff fe` is a 12-byte keepalive complete in the header
- [X] T042 [P] [US2] Implement the MSB-first bit reader in `sc-capture/pkg/scproto/bitreader.go` for bit-packed bodies
- [X] T043 [US2] Generate golden vectors into `sc-capture/testdata/golden/` from `docs/protocol/source/protocol.py` (by hand, once) and assert parity in `sc-capture/pkg/scproto/golden_test.go` — framing, checksum, and the little-endian header trap specifically

### Flow identification

- [X] T044 [US2] Implement TCP reassembly in `sc-capture/internal/decode/reassembly.go` using gopacket `reassembly`, consuming journaled frames — **never the live socket** (Principle II)
- [X] T045 [P] [US2] Implement the flow table in `sc-capture/internal/flow/table.go` — `conn_id`, endpoints, direction, first/last seen, reassembly state
- [X] T046 [US2] Implement service classification in `sc-capture/internal/flow/classify.go` — ports 3801/3802/3815 as `port` evidence, anything else game-adjacent as `unknown` and never dropped (FR-005)
- [X] T047 [US2] Implement handoff detection in `sc-capture/internal/flow/handoff.go` — decode `SCMD_CONNECT_DEDICATED_SERVER` (type 11: cstring addr, u16 port, u64 session_id, i32 zone_id, u1 flag) and classify the resulting UDP flow as `dedicated` with `handoff` evidence (FR-011)
- [X] T048 [US2] Record `connections[]` and `services_observed` into `session.json` per contracts/session.schema.json
- [X] T049 [US2] Record the desync point in `connections[].desync_at_frame` when reassembly fails, while capture continues unbroken (spec edge case)

**Checkpoint**: In-match traffic is archived — the single irreplaceable gap, closed without depending on relay feasibility.

---

## Phase 6: User Story 4 — Understand a session without having been there (Priority: P2)

**Goal**: A reader can see which messages were exchanged, what they were called, and what the player was doing, with no contributor narration.

**Independent Test**: One person captures a known action sequence without notes; a second reconstructs it from the session alone.

- [X] T050 [US4] Implement the record index writer in `sc-capture/internal/index/writer.go` — `index.jsonl`, append-only, fsync on the journal's cadence, conforming to `contracts/index.schema.json`
- [X] T051 [US4] Assemble records in `sc-capture/internal/decode/record.go` — protocol unit boundaries, `(segment, index)` frame citations, both clocks per record (FR-003, FR-007)
- [X] T052 [US4] Implement decode dispatch in `sc-capture/internal/decode/decode.go` — label message type and sub-type element names (FR-015)
- [X] T053 [US4] Implement per-record panic containment in `sc-capture/internal/decode/isolate/isolate.go` — recover, record `status: failed`, never propagate (FR-016)
- [X] T054 [US4] Implement the four-way decode status in `sc-capture/internal/decode/status.go` — `decoded`, `undecoded`, `unknown_element`, `failed`; never present a partial interpretation as complete (FR-017, FR-022)
- [X] T055 [P] [US4] Derive annotations from decoded traffic in `sc-capture/internal/annotate/derived.go`, tagged `source: system` so they are never confused with contributor testimony (FR-019)
- [X] T056 [US4] Wire `sccap decode` in `sc-capture/cmd/sccap/decode.go` with read-time filters (`--conn`, `--type`, `--status`, `--json`); undecodable records print as raw bytes with frame references, never omitted
- [X] T057 [US4] Set `credential_warning` when `CCMD_AUTH_REQUEST` (type 4) or `AC_PLAYER_CREDENTIALS` (opcode 9) is observed, and tell the contributor at close — **completes US6/FR-033**

### Tests for User Story 4 (constitution-mandated)

- [X] T058 [US4] Build the fault-injection corpus in `sc-capture/testdata/corpus/` — decoder panic, malformed frame, mid-stream desync, unknown message type, unknown `AC_*` opcode
- [X] T059 [US4] Implement the fault-injection suite in `sc-capture/tests/faultinjection/faultinjection_test.go`, asserting for every case: pcapng bytes identical to the no-fault control, session not terminated, relay uninterrupted, the offending record present with `failed`/`unknown_element`, and the desync point recorded (SC-003, Principle II)

**Checkpoint**: Sessions are self-explanatory, and Principle II is proven rather than asserted.

---

## Phase 7: User Story 3 — Know what has never been observed (Priority: P2)

**Goal**: The contributor gets a concrete list of protocol elements never seen, and it shrinks as they exercise the game.

**Independent Test**: Capture two sessions exercising different parts of the game. Assert the never-observed set shrinks by exactly the elements that appeared, persists across restarts, and aggregates across sessions.

- [X] T060 [US3] Implement the coverage store in `sc-capture/internal/coverage/store.go` — single JSON document under `$XDG_DATA_HOME/sccap/`, written by temp-file plus atomic `rename(2)` (research.md R7)
- [X] T061 [US3] Implement the state lattice in `sc-capture/internal/coverage/state.go` — `never_observed → observed_undecoded → decoded`, strictly one-directional so a later failed decode never downgrades an element (FR-020, FR-024)
- [X] T062 [P] [US3] Flag elements absent from the embedded universe as novel in `sc-capture/internal/coverage/novel.go`, and surface them during capture (FR-023)
- [X] T063 [P] [US3] Write `coverage-delta.json` into each bundle so machine-wide coverage can be rebuilt from bundles alone
- [X] T064 [US3] Wire `sccap coverage` in `sc-capture/cmd/sccap/coverage.go` with `--kind`, `--state`, `--json` and `--ingest`, reporting the three-way split against 404 known elements
- [X] T065 [P] [US3] Surface the live novel-element count on the status line and print one persistent line per discovery
- [X] T066 [P] [US3] No-regression test in `sc-capture/internal/coverage/state_test.go` — an element that decoded in an earlier session stays `decoded` after a later session fails to decode it

**Checkpoint**: The project has a progress metric and a completion condition.

---

## Phase 8: User Story 5 — Re-decode the archive after the servers are gone (Priority: P3)

**Goal**: An improved decoder applied to an old session extracts meaning that was not understood at capture time, without touching the raw journal.

**Independent Test**: With all game services unreachable, decode an archived session and reproduce the capture-time results. Extend a decoder, confirm previously undecoded records now decode, and confirm the raw journal is unmodified.

- [X] T067 [US5] Implement `sccap index --rebuild` in `sc-capture/cmd/sccap/index.go`, regenerating `index.jsonl` from pcapng segments alone and leaving segment hashes unchanged (FR-030)
- [X] T068 [P] [US5] Stamp `decoder_version` on every decode result so a later re-decode can be compared against what was recorded at capture time (SC-007)
- [X] T069 [US5] Enforce schema-version handling in `sc-capture/internal/session/schema.go` — read known MAJOR while ignoring unknown fields; refuse unknown MAJOR with a diagnostic naming both versions and exit 5, never a partial read (FR-027)
- [X] T070 [US5] Offline reproducibility test in `sc-capture/tests/e2e/offline_test.go` — with no network, assert decode output is identical across a rebuild and that raw segment hashes are unchanged
- [X] T071 [P] [US5] Tolerant index parsing — a truncated final line in `index.jsonl` is detected and does not invalidate the session (research.md R3)

**Checkpoint**: The archive outlives the servers, and improving a decoder is a one-command operation.

---

## Phase 9: Polish & Cross-Cutting Concerns

- [X] T072 [P] Wire `sccap status` in `sc-capture/cmd/sccap/status.go`, reading the live session state file without perturbing capture
- [X] T073 [P] Document the bundle format and element universe in `sc-capture/README.md`, linking `docs/protocol/PROVENANCE.md`
- [X] T074 [P] Produce cross-compiled release artifacts for linux/amd64 and linux/arm64 in CI
- [ ] T075 Run the full [quickstart.md](./quickstart.md) validation, Scenarios 1–10
- [ ] T076 Two-person validation for SC-010 (reconstruct an action sequence from a session alone) and SC-012 (open a bundle in third-party tooling on a machine with none of this project's software)

---

## Phase 10: User Story 2b — Relay feasibility spike (Priority: P1, time-boxed)

**Goal**: Establish whether a match can be joined through a relay, and at what cost. **Disproving this is a valid and useful outcome** — passive capture already archives in-match traffic, so nothing depends on it succeeding.

**Independent Test**: Attempt a relayed match in a non-competitive mode. Either it works within the latency budget, or the attempt and its rejection are fully recorded and the contributor is told plainly.

**Runs last on purpose**: it is the only work here that may return nothing, and it is the only work that touches live traffic in a way that could be read as tampering.

- [ ] T077 [US2] Implement the lb/shard/chat interposition overlay in `sc-capture/internal/relay/relay.go`, relaying byte-for-byte except for enumerated address rewrites (FR-009)
- [ ] T078 [US2] Record every rewrite performed into `session.json.rewrites[]` with its rationale (FR-010)
- [ ] T079 [US2] Capture both legs when relaying — bind `lo` and the uplink, emit an IDB per interface, and assert both are present in verification (plan Risk 3)
- [ ] T080 [US2] Implement the UDP dedicated-server relay spike in `sc-capture/internal/relay/udp.go`, recording the attempt, any rejection, and all traffic leading to it (US2 acceptance 3)
- [ ] T081 [US2] Measure added round-trip against an unrelayed baseline and report p99 in `sc-capture/internal/relay/latency.go`; ≤ 15 ms is the pass condition (SC-006)
- [ ] T082 [US2] Write the feasibility verdict into the session and tell the contributor plainly whether the relay is viable, rather than leaving them with a broken client

**Checkpoint**: The relay question is answered either way, with evidence.

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: no dependencies
- **Foundational (Phase 2)**: depends on Setup — **blocks everything**
- **US1 (Phase 3)**: depends on Foundational. Needs no protocol code
- **US6 (Phase 4)**: depends on Phase 3 for the session writer; T034 could land inside Phase 3
- **US2a (Phase 5)**: depends on Phase 3 (journal to read from). Introduces `pkg/scproto`
- **US4 (Phase 6)**: depends on Phase 5 (framing and reassembly)
- **US3 (Phase 7)**: depends on Phase 6 — coverage is computed from decoded records
- **US5 (Phase 8)**: depends on Phase 6
- **Polish (Phase 9)**: depends on the stories you intend to ship
- **US2b (Phase 10)**: depends on Phase 5; independent of everything after it

### Critical path

```
Setup → Foundational → US1 (MVP) → US2a → US4 → US3
                          └→ US6        └→ US5
                                        └→ US2b (spike, optional)
```

### Within each story

- Protocol primitives before consumers: T040/T041 before T044–T047
- Journal before decode, always — never the reverse (Principle II)
- `internal/session/metadata.go` (T022) is touched by several stories; changes there are sequential

### Parallel opportunities

- Setup: T003–T007 all parallel
- Foundational: T009–T012 and T016 parallel; T013/T014 sequential (same package)
- US1: T031–T033 parallel once implementation lands; T018 and T023 parallel with the writer work
- US6: T035–T037 fully parallel
- US2a: T038, T039, T042 parallel; T045 parallel with protocol work
- US3: T062, T063, T065, T066 parallel

---

## Parallel Example: Phase 2 Foundational

```bash
# Four independent packages, no shared files:
Task: "Exit codes in sc-capture/internal/exitcode/exitcode.go"
Task: "Clock anchors in sc-capture/internal/session/clock.go"
Task: "Disk monitoring in sc-capture/internal/session/disk.go"
Task: "Status line in sc-capture/internal/status/status.go"
```

## Parallel Example: Phase 5 protocol primitives

```bash
# Tables, types and the bit reader share no state:
Task: "Embed element tables in sc-capture/pkg/scproto/tables.go"
Task: "Message and opcode types in sc-capture/pkg/scproto/types.go"
Task: "MSB-first bit reader in sc-capture/pkg/scproto/bitreader.go"
```

---

## Implementation Strategy

### MVP first (Phases 1–4)

1. Setup + Foundational — `sccap doctor` tells you whether this machine can capture
2. US1 — capture, verify, mark
3. US6 — safety properties, true from the first byte
4. **STOP and VALIDATE**: run quickstart Scenarios 1, 2, 4, 8, 9
5. Ship it to contributors and **start capturing**

T001–T037 is the whole MVP. It contains no protocol code, which is why it can land fast — and it is the only part with a hard deadline attached.

### Incremental delivery

Each phase after the MVP leaves the tool working and adds value without breaking what came before:

| After phase | A contributor can |
|---|---|
| 4 | Archive verified, sensitive-by-default sessions |
| 5 | Archive in-match traffic, identified from the protocol |
| 6 | Read a session without having been there |
| 7 | See what has never been observed, and go get it |
| 8 | Re-decode any archive, years later |
| 10 | Know whether a relayed match is possible at all |

### Parallel team strategy

After Foundational, US1 is the bottleneck — everything reads the journal it produces. With more than one person, the second should take `pkg/scproto` (T038–T043) immediately, since it is pure, testable against golden vectors, and needs no capture hardware.

---

## Notes

- `[P]` = different files, no dependencies on incomplete work
- Every increment must leave the tool able to produce a valid archived session (Principle VII)
- Commit after each task or logical group; every PR declares its effect on the archive
- **Capture something before T001.** The MVP is days away and the deadline is not; `dumpcap` on this machine already writes the same pcapng bytes that Phase 3 will
