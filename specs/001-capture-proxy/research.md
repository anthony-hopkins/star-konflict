# Phase 0 Research: Comprehensive Protocol Capture Proxy

**Feature**: `001-capture-proxy` | **Date**: 2026-08-13 | **Spec**: [spec.md](./spec.md)

Every unknown in the plan's Technical Context is resolved below.

**Revision note (2026-08-13)**: the sibling repositories were removed from the workspace after
this research was first written, and the constitution was amended to v2.0.0. R6 and R7 are
revised accordingly; the rest stands unchanged, because nothing else depended on them. Findings
are grounded in the capture manual (`docs/Star-Conflict-Capture-Protocol.md`), the frozen
protocol snapshot (`docs/protocol/`), and both former repositories as read before removal.

---

## R1. Raw journal container format

**Decision**: pcapng, written with `github.com/gopacket/gopacket/pcapgo.NgWriter`, one section
per session, one IDB per captured interface, nanosecond timestamp resolution (`if_tsresol = 9`),
segmented into ~200 MB / 10 min files named `capture_NNNNN_<UTC>.pcapng`.

**Rationale**: The clarification session settled on a standard container plus a documented
sidecar. pcapng is the only format that is simultaneously (a) universally readable — Wireshark,
tshark, scapy, and every future tool that reads captures at all, (b) already the format the
project's own capture manual mandates (`docs/…§2.1`), so bundles from this tool and bundles from
`dumpcap` are interchangeable at intake, and (c) capable of carrying interface metadata, drop
counters and per-section options natively. The segment naming above is copied verbatim from
`docs/…§2.1`, so a bundle written by `sccap` is indistinguishable at intake from one produced by
the manual's original procedure.

**Alternatives considered**: Classic `.pcap` — no nanosecond resolution option, no interface
metadata, no drop counters. A project-defined framing log — rejected by clarification; it makes
the archive hostage to one binary still compiling. pcapng with all metadata in `opt_comment`
options — rejected: comment strings are unstructured, slow to scan, and Wireshark truncates
them in display; a sidecar is greppable and schema-validated.

---

## R2. Where capture happens: wire-level vs. in-proxy (central decision)

**Decision**: Capture at the wire, with `AF_PACKET`, always. The proxy/relay is an **optional
overlay**, not the capture mechanism. Passive wire capture is the default mode.

**Rationale**: This is forced by three independent constraints that all point the same way.

1. **FR-004 requires complete transport framing including header fields and checksums.** A
   userspace MITM proxy *terminates* TCP. The TCP headers it sees on the client side are its own
   kernel's, not the real server's; sequence numbers, window sizes, retransmissions, MSS and
   timing are all synthesised by the relay. A proxy physically cannot satisfy FR-004 for the
   real connection. Only a packet-level capture can.
2. **SC-002 requires the journal to be byte-identical to an independent packet capture.** If the
   journal *is* a packet capture taken the same way, the criterion is satisfied by construction
   rather than by careful re-synthesis of fake frames.
3. **Principle II forbids any position where a decoder failure loses bytes.** With wire capture
   the decoder sits *downstream* of the journal, reading records that are already durable. It is
   then architecturally incapable of losing a byte — not merely careful about it. A decode path
   that hangs, panics or desyncs cannot affect what was written, because writing already
   happened on a different path.

The existing Python proxy is the counter-example the constitution was written against: it
decodes in-path and stores only decoded bodies, which is why tick rate, latency and
retransmission are unanswerable from its captures.

A second consequence is that **in-match UDP no longer depends on the relay working**. Passive
capture records the dedicated-server traffic whether or not a relayed connection would be
accepted, which removes the spec's single largest feasibility risk from the critical path. The
relay question (can a match be joined *through* a proxy?) becomes a bounded spike whose failure
costs nothing, exactly as Principle "feasibility spikes precede design" intends.

**Alternatives considered**: Proxy-only capture, synthesising pcapng frames from relayed payload
— fails FR-004, weakens SC-002 to a self-referential check, and puts a decoder in the byte path.
Both, with the proxy as primary and wire capture as verification — doubles the storage, doubles
the failure modes, and still leaves the primary journal unable to answer transport questions.
eBPF/`SO_TIMESTAMPING` socket-level capture — richer syscall correlation but kernel-version
fragile, and the manual already covers that as an optional Tier-3 technique.

**Consequence for FR-008 (interpose on every service)**: retained as a capability, but capture
no longer *depends* on it. This is a deliberate shift of emphasis from the spec's proxy-centric
wording and is called out in the plan's design-decisions note.

---

## R3. Sidecar index format

**Decision**: `index.jsonl` — JSON Lines, one object per protocol-level record, appended as
capture proceeds, fsynced on a cadence. It is **derived data and fully regenerable** from the
pcapng via `sccap index --rebuild`.

**Rationale**: JSONL is append-only, so an abrupt kill truncates at most the final line and the
truncation is detectable by a failed parse of that one line — which is precisely the durability
shape FR-006 and SC-008 ask for. It streams, it greps, it needs no reader library, and a schema
can validate it. Declaring it derived rather than primary is the important part: it means a
corrupt, truncated or entirely absent index never invalidates a session, and it is what makes
FR-030 (apply an improved decoder to an archived session without touching raw records) a
one-command operation rather than a migration.

**Alternatives considered**: SQLite index — better random access, but a mid-write kill can leave
a locked or partially-committed database, and it makes the sidecar opaque to anyone without the
tooling. Parquet/columnar — good for analysis, bad for append-during-capture and crash tails.
pcapng `opt_comment` per packet — see R1.

---

## R4. Bridging wall-clock and monotonic time

**Decision**: pcapng frames carry the kernel's wall-clock timestamp. Every `index.jsonl` record
carries both `t_wall` (UTC, ns) and `t_mono` (ns since session start). The two are tied together
by **clock anchors** — paired `CLOCK_REALTIME`/`CLOCK_MONOTONIC` readings written to
`session.json` at session start, every 30 s, and at session end.

**Rationale**: FR-003 demands both clocks per record; pcapng has a slot for only one, and
`AF_PACKET` hands up a realtime timestamp. Anchors let any frame's monotonic time be recovered by
interpolation, and — the point of the edge case — a wall-clock step mid-session shows up as a
discontinuity *between anchors*, leaving monotonic ordering intact and the step itself recorded
rather than silently corrupting the timeline. `session.json` already carries a `clock` block in
the existing manual (`docs/…§2.4`); this extends it rather than inventing a parallel concept.

**Alternatives considered**: `SO_TIMESTAMPING` with `CLOCK_MONOTONIC` hardware/software stamps —
more precise, but availability varies by NIC and driver, and it would make the timestamp source
non-uniform across contributor rigs. Anchoring is uniform and rig-independent.

---

## R5. Go capture stack

**Decision**: `github.com/gopacket/gopacket` v1.7.1 (the maintained fork) — `afpacket` for the
`AF_PACKET` v3 ring buffer, `pcapgo.NgWriter` for pcapng output, `reassembly` for TCP stream
reassembly in the decode layer. No `libpcap`/cgo. Verified fetchable from this workspace.

**Rationale**: Pure-Go `AF_PACKET` keeps `CGO_ENABLED=0` static single-binary distribution and
cross-compilation intact, which is the constitution's stated reason for choosing Go. The v3 ring
buffer gives kernel-side batching and, importantly, exposes `Stats()` drop counters — the
zero-loss assertion in SC-002/SC-003 needs a number to assert on, and this is where it comes
from. gopacket's `reassembly` package is used **only** in the decode layer, downstream of the
journal, where a desync degrades to "bytes stored, meaning unknown" instead of losing anything.

**Alternatives considered**: `libpcap` via cgo — breaks static cross-compilation for
non-expert contributors. Raw `AF_PACKET` syscalls by hand — no benefit over a maintained
wrapper and a fresh source of bugs in the one component that must not have any. Shelling out to
`dumpcap` — the manual already does that; a tool that only wraps it adds nothing and cannot
correlate live decode.

---

## R6. The protocol implementation (Principle VI)

> **Revised 2026-08-13.** The sibling repositories were removed from the workspace and the
> constitution was amended (v2.0.0) to redefine Principle VI as "one implementation *here*"
> rather than "shared with the server". The original decision — importing the Go server's
> `starconflict/lib` via a `replace` directive under EUPL-1.2 — is withdrawn.

**Decision**: Implement framing, message typing, the variant-map encoding and the bit reader in
`sc-capture/pkg/scproto`, with no dependency on `internal/`, no dependency outside the standard
library, and no second copy anywhere in the repo. The element tables are `go:embed`ed from the
frozen snapshot in `docs/protocol/`. The repository is free to choose its own licence.

**Rationale**: The cross-repo dependency was never actually available — the Go server declared
the unresolvable module path `module starconflict`, and its parser took a `net.Conn` rather than
a byte source, which is the wrong shape for parsing already-journaled bytes. Removing it deletes
the plan's only external prerequisite and its only copyleft constraint. The intent of Principle
VI is preserved by keeping `scproto` dependency-free: a future server reimplementation can
consume it unchanged, which is the same guarantee from the other direction.

The protocol knowledge itself is not lost and does not need re-deriving. It is public domain
(sc-proxy is Unlicense) and now snapshotted in `docs/protocol/` — 404 named elements plus the
Kaitai schemas, the Ghidra pointer listing, and `protocol.py`, which is a complete reference
implementation of framing and the checksum.

**What must be re-implemented, and the traps in each**:

| Construct | Detail that must not be got wrong |
|---|---|
| Header | 12 bytes, **big-endian**: `[4B body_len][2B seq][2B echo_seq][2B cmd_type][2B checksum]` |
| Checksum | MurmurHash2, seed `0x1337533d`, `m = 0x5bd1e995`, `h` initialised to `12 ^ seed`. The header is fed to the hash **little-endian** even though the wire is big-endian — both reference implementations agreed on this, and it is the single likeliest source of a silent mismatch |
| Special frames | `body_len > 0xfffffc` is not a length. `ff ff ff ff` is a disconnect carrying a reason byte; `ff ff ff fe` is a 12-byte keepalive that is complete in the header — reading 8 further bytes over-reads into the next frame and desyncs the stream permanently |
| Variant map | Tag order: `nil, i32, u64, unknown, f32, str, dict, unknown2, bool` |
| Bit reader | MSB-first within each byte; used for bit-packed bodies such as `SCMD_ASSIGNED_SHARD` |

**Verification**: golden vectors generated from `docs/protocol/source/protocol.py` and checked
in, so agreement with the reference is a test result rather than a claim. An architecture test
asserts `pkg/scproto` imports nothing from `internal/`, which is what keeps it consumable and
keeps a second parser from appearing behind a convenience wrapper.

**Alternatives considered**: Keep the `replace` dependency on a sibling checkout — rejected by
the user's decision to work independently, and it never built from a fresh clone anyway. Vendor
the Go server's EUPL-1.2 packages — would impose copyleft on this repository for code that is
straightforward to write, and the Unlicense material covers the same ground freely. Re-extract
the element names from the game binary — authoritative but a separate reverse-engineering effort;
the snapshot is re-derivable from `source/AC_ptrs` if it is ever doubted.

---

## R7. Coverage state store

**Decision**: A single JSON document at `${XDG_DATA_HOME:-~/.local/share}/sccap/coverage.json`,
written by atomic temp-file + `rename(2)`. Bootstrapped from the known element universe:
39 message types, 249 `AC_*` opcodes, 116 `SN_*` notification types.

**Rationale**: The dataset is ~404 elements with three states each. That is kilobytes. A
database buys nothing and costs a dependency, a schema migration story, and a corruption mode on
abrupt termination. Atomic rename gives crash-safety for free. Human-readable coverage state is
also an asset for a volunteer project — a contributor can read, diff, and share it.

Element universe source: the frozen snapshot in `docs/protocol/` —
`message-types.json` (39), `async-requests.json` (249), `notifications.json` (116) — embedded
into the binary with `go:embed` so coverage works on a machine with nothing else installed. See
`docs/protocol/PROVENANCE.md` for origin and licence.

**Alternatives considered**: `modernc.org/sqlite` (pure Go, verified fetchable) — viable, and the
right answer if coverage ever grows per-field rather than per-element, but currently a
20 MB binary-size and build-time tax for a 400-row table. BoltDB — opaque to humans, same
objection.

---

## R8. Reaching in-match UDP

**Decision**: Passive wire capture is the primary and default acquisition path for
dedicated-server traffic. Flows are identified by parsing `SCMD_CONNECT_DEDICATED_SERVER`
(message type 11) out of the master-server stream, which yields `addr`, `port`, `session_id`
(u64) and `zone_id`. Its body layout is `cstring addr + u16 port + u64 session_id + i32 zone_id +
u1 flag`, recorded in `docs/protocol/source/scmd_decoders.py`. A UDP relay is built **only** as a
time-boxed feasibility spike, run against non-competitive modes.

**Rationale**: The spec's assumption is that the u64 `session_id` may be bound to the address the
master server registered, so a relayed connection may be refused. Passive capture makes that
question irrelevant to whether the traffic gets archived, which converts the project's largest
unproven dependency into a research curiosity. It is also the strictly safer path under Principle
IV: no rewriting means nothing that could be read as tampering.

**Alternatives considered**: Relay-first — the spec's original framing; retained as a spike, but
making the irreplaceable capability depend on an unverified assumption is exactly the risk the
constitution's spike rule exists to prevent. Flow identification by port heuristics alone —
insufficient; the handoff message is the ground truth and is already decodable.

---

## R9. Durability and abrupt termination

**Decision**: pcapng segments are written through a buffered writer flushed and `fsync`ed every
1 s or 4 MB, whichever comes first; `index.jsonl` is appended and fsynced on the same cadence.
Segment rotation closes and fsyncs the outgoing file before opening the next. `SHA256SUMS` is
written at clean shutdown; `sccap verify` recomputes and, for a session with no `SHA256SUMS`,
reports it as *interrupted but verifiable*, hashing what is present.

**Rationale**: FR-006 sets the bound at "at most the records in flight", which is what a 1 s
flush cadence delivers. pcapng's block structure is self-delimiting, so a truncated tail is
detectable by a structural walk rather than corrupting earlier blocks — a partial session stays
readable up to the failure point, which is SC-008.

**Alternatives considered**: fsync per packet — correctness is identical and throughput is not;
at a few thousand packets/second this is the difference between working and dropping. No fsync
until close — loses minutes on power loss.

---

## R10. Disk exhaustion behaviour

**Decision**: `--min-free` (default 2 GiB) warning threshold and a hard floor (default 512 MiB).
Crossing the warning threshold emits a visible, repeated warning. Reaching the floor closes the
session cleanly — final flush, `session.json` completion, `SHA256SUMS` — and stops capture with a
non-zero exit and an explicit message. Prior sessions are never touched.

**Rationale**: Settled in clarification. The design point is that the floor is reached with
enough headroom to *finish writing metadata*, which is why the floor is well above zero: a clean
close needs space for `session.json` and `SHA256SUMS`.

---

## R11. Live progress reporting (deferred from clarification)

**Decision**: A single periodic status line on stderr, refreshed once per second: elapsed,
services seen, packets captured, bytes journaled, kernel drops, records decoded, and a count of
novel elements. Novel-element discoveries additionally print one persistent line each. No TUI, no
extra dependency. `sccap status` prints the same snapshot for a running session.

**Rationale**: FR-035 asks the system to show it is working; SC-001 asks a novice to reach a
verified session in 15 minutes. A status line satisfies both. A TUI would add a dependency and a
terminal-compatibility surface to a tool whose job is to not interfere with a running game.
Kernel drops are on the line deliberately: it is the number that tells a contributor their
capture is worthless before they spend an hour on it.

---

## R12. Session identity and naming (deferred from clarification)

**Decision**: Reuse the existing bundle convention verbatim —
`SC_<UTC_START>__<SCENARIO_ID>__<VOLUNTEER_ID>__<REGION>__<SEQ>` (`docs/…§2.2`). `sccap` generates
it, defaulting `SCENARIO_ID` to `ADHOC` when the contributor does not name one, and
`VOLUNTEER_ID` to `vol-local`. The directory name is the session identity; `bundle_id` inside
`session.json` repeats it.

**Rationale**: Bundles from this tool and from the existing shell tooling should be
indistinguishable at intake. Inventing a second naming scheme would fork the archive's index for
no gain.

---

## R13. Protection at rest

**Decision**: Session directory `0700`, all files `0600`, set at creation via `umask` within the
session writer rather than `chmod` afterwards. No encryption. `session.json` carries
`"sensitive": true` and a `sensitivity_reason` string; the credential warning fires when the
decode layer observes `AC_PLAYER_CREDENTIALS` (opcode 9) or a `CCMD_AUTH_REQUEST` (type 4).

**Rationale**: Settled in clarification — encryption would put a key between a future reader and
the raw evidence, which undercuts Principle II and adds an archive-destroying failure mode.
Setting the mode at creation rather than after closes the window where a session is briefly
world-readable. The credential warning is tied to concrete, already-decoded messages rather than
a heuristic.

---

## R14. Compatibility with the existing capture protocol, and a gap found

**Decision**: `sccap` emits a bundle that is a **superset** of the manual's bundle
(`docs/…§2.1`): same directory naming, same `session.json` (extended, `schema_version` bumped),
same `markers.log` semantics, same `SHA256SUMS`, plus `index.jsonl` and `coverage-delta.json`.
`dumpcap.log` is replaced by an equivalent `capture.log` carrying `AF_PACKET` stats, and
`session.json.host.capture_tool` records `sccap <version>` instead of Dumpcap.

**The `tools/` gap, and how it is closed** *(resolved 2026-08-14)*: the manual and the root
`README.md` both reference a `tools/` directory (`setup-ubuntu.sh`, `netns-capture.sh`,
`sc-marker.py`, `verify_capture.py`, `session.schema.json`) that does not exist in this
workspace. Under the Go-only rule (constitution v2.1.0) it is not recreated. Each former helper
is dispositioned:

| Former helper | Disposition |
|---|---|
| `sc-marker.py` | → `sccap mark`, in Go. Better placed: it can stamp against the session's own clock anchors instead of guessing from outside |
| `verify_capture.py` | → `sccap verify`, in Go. Better placed: it reads the session's own recorded drop counters rather than parsing a log |
| `session.schema.json` | → `contracts/session.schema.json`, reconstructed from the manual's §2.4 example. If an authoritative copy surfaces, reconcile against it |
| `setup-ubuntu.sh` | **Not recreated.** Host setup is out of scope; `sccap doctor` detects each condition it used to configure and names the remedy |
| `netns-capture.sh` | **Not recreated.** Same; the completeness guarantee it provided by construction is replaced by `sccap doctor --watch` detecting which interfaces actually carry game traffic (plan Risk 4) |

This is a net loss of automation and a net gain in honesty: a script that configures a host can
be run and still leave a capture compromised, whereas detection makes the compromise visible.
Automating the fix was optional; noticing the problem is not.

---

## R15. Licence

**Decision**: MIT, for `sc-capture`.

**Rationale**: The copyleft constraint vanished with the EUPL dependency (R6), and the element
tables are public domain, so nothing is imposed from outside. The constitution's stated goal is
an archive and tooling that "outlive the project and its authors" — which argues for the licence
that an unknown future maintainer can act on without legal analysis. MIT is the most widely
understood of those, compatible with everything, and imposes no obligation on a future server
reimplementation that consumes `pkg/scproto` — which is the mechanism Principle VI now relies on.

**Alternatives considered**: Apache-2.0 — adds an explicit patent grant, valuable for commercial
adoption, irrelevant here and longer to read. Unlicense/CC0 — matches the prior art exactly and
is maximally permissive, but public-domain dedication is not clean in every jurisdiction, and MIT
achieves the practical goal without that ambiguity. EUPL-1.2 — no longer forced, and copyleft
would work against a future maintainer picking the code up.

**Rationale**: Interchangeability at intake is worth more than a cleaner greenfield layout, and
the marker beacon's three-way video/packet binding is a genuinely good design that this tool
should adopt rather than replace — `sccap mark` writes the same `markers.log` line format and
emits the same broadcast datagram, so it lands inline in the pcapng exactly as `sc-marker.py`
does today.

---

## Resolved unknowns summary

| Unknown | Resolution |
|---|---|
| Journal container | pcapng, segmented, ns resolution (R1) |
| Capture point | Wire-level `AF_PACKET`; relay optional and off by default (R2) |
| Sidecar format | `index.jsonl`, derived and regenerable (R3) |
| Dual timestamps | pcapng wall-clock + index monotonic, tied by clock anchors (R4) |
| Go stack | gopacket 1.7.1 afpacket/pcapgo/reassembly, `CGO_ENABLED=0` (R5) |
| Protocol implementation | Self-contained `pkg/scproto`, golden vectors from the archived reference (R6) |
| Coverage store | Atomic-rename JSON, 404 elements embedded from `docs/protocol/` (R7) |
| In-match UDP | Passive primary; relay is a spike (R8) |
| Durability | 1 s / 4 MB flush+fsync cadence (R9) |
| Disk floor | Warn at 2 GiB, clean stop at 512 MiB (R10) |
| Progress UI | One stderr status line, no TUI (R11) |
| Session naming | Existing `SC_…` bundle convention (R12) |
| At-rest protection | `0700`/`0600`, no encryption (R13) |
| Bundle compatibility | Superset of the manual's bundle; former `tools/` helpers absorbed or dropped (R14) |
| Licence | MIT (R15) |
| Host setup | Diagnosed by `sccap doctor`, never orchestrated; no scripts ship (R14, constitution v2.1.0) |
