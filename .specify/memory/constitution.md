# Star Conflict Preservation Constitution

Governing principles for work in this repository: capturing, decoding and reimplementing the
Star Conflict network protocol before the official servers shut down.

**Scope.** This constitution binds work authored here. As of v2.0.0 this project is
self-contained: the Python decoder (`sc-re/sc-proxy`, Unlicense) and the Go server
reimplementation (`sc-re/star-conflict-revitalized`, EUPL-1.2) are external projects that no
longer live here and that this document does not govern. Protocol knowledge recovered by them
remains available as public-domain prior art — a frozen snapshot is kept in `docs/protocol/` —
but no code dependency on either is required, expected, or currently held.

**The governing fact.** This project has a hard external deadline it does not control. Every
principle below exists because violating it produces an archive that cannot be repaired once
the servers are gone.

## Core Principles

### I. Capture Everything, Decide Later (NON-NEGOTIABLE)

The system MUST NOT discard, filter, sample, or truncate observed traffic at capture time.
Every byte crossing an interposed connection MUST be persisted — including traffic that cannot
be parsed, cannot be named, and was not expected.

Filtering is permitted only at read time, against an already-complete archive.

**Rationale:** Capture-time decisions are irreversible. The traffic most likely to be dropped by
a rule written today is exactly the traffic nobody predicted — an unknown opcode, a fallback
path, a renegotiation. We do not learn what mattered until after the servers are gone, when
nothing can be re-captured. Disk is cheap; the window is not.

### II. Raw Bytes Are Evidence; Decoded Output Is Derived

Every session MUST persist an unmodified, timestamped, byte-exact journal of wire traffic,
independent of any decoder. Decoded artifacts MUST be reproducible from that journal alone.

No decoder may occupy a position where its failure causes byte loss. Decoder failure — panic,
desync, unknown type, malformed frame — MUST degrade to "bytes stored, meaning unknown" and MUST
NOT abort the session, break a relay, or omit a record.

**Rationale:** This is the known failure mode of the existing Python proxy, which stores only
decoded message bodies: a mis-framed stream is silently and permanently wrong, undetectable
afterwards. Derived data cannot be falsified against itself. Raw bytes can be re-parsed by any
future decoder, including one written after this project ends.

### III. Observation Is Deadline-Bound; Decoding Is Not

Where breadth of observation and depth of decoding compete for effort, breadth MUST win while
the servers are up.

The system MUST track persistently, across all sessions, which protocol elements have **never
been observed**, and MUST report that set on demand. This is a first-class capability, not
diagnostics.

**Rationale:** An opcode never captured dies with the servers. An opcode captured but not
understood is safe forever. Roughly 249 async-request opcodes are known by name and about 32
have decoded bodies — but the number that governs urgency is how many have never appeared on
the wire at all, and nothing currently measures it. Knowing what is still missing converts
capture from an open-ended chore into a closed loop with a completion condition.

### IV. Do No Harm to the Live Service

The system MUST default to observing without altering. Traffic MUST be relayed byte-for-byte
except for a small, explicitly enumerated, individually documented set of address rewrites
required to route connections through the proxy.

The system MUST NOT fuzz, replay, inject, or synthesise traffic toward live servers. Any in-path
behaviour that could plausibly be read as tampering MUST be disableable, and MUST NOT be
required in order to contribute.

**Rationale:** A banned account is a contributor permanently removed from a project with a
deadline, and a disrupted match harms uninvolved players. Passive contribution must remain a
complete, first-class path. The proxy earns adoption by being useful, never by being mandatory.

### V. Every Artifact Is Self-Describing and Verifiable

Every session MUST carry structured metadata sufficient to interpret it without asking its
author: schema version, software version, client build, start and end in UTC, clock discipline,
and which connections were interposed. Every session MUST carry integrity hashes.

Timestamps MUST be recorded per record, from both a wall-clock and a monotonic source.

**Rationale:** Contributions arrive from distributed volunteers and must outlive the project and
its authors. An archive requiring tacit knowledge to read stops being readable. Per-record
timestamps are also the only way to answer tick rate, latency, keepalive cadence and
retransmission — questions the current tooling cannot answer at all, because it stores no
timestamps.

### VI. One Protocol Implementation

There MUST be exactly one implementation of framing, message typing, and common value encodings
in this repository. It MUST live in a single package with no dependency on capture, storage, or
transport, so that a future server reimplementation can consume it unchanged. A second parser of
the same construct — for tests, for tooling, for convenience — is a defect.

**Rationale:** An emulator must eventually serve exactly the bytes this tooling observes. Two
independent parsers of one protocol drift, and the drift surfaces later as bugs that are
expensive to attribute. Keeping the implementation in one dependency-free package turns
disagreement into a compile-time or test-time event instead of a mystery, and keeps the option of
sharing it open without requiring a sibling project to exist today.

### VII. Ship in Independently Useful Increments

Every increment MUST be usable by a contributor the day it lands. No increment may require a
later increment in order to produce a valid archived session.

Work that improves the archive retroactively MUST be prioritised over work that only improves
convenience.

**Rationale:** The characteristic failure of preservation projects is spending the remaining
window building tooling instead of capturing. If the servers shut down mid-development, whatever
shipped must already have been collecting data.

### VIII. Contributor Safety Is Part of the Product

Captured sessions contain authentication material and account identifiers. The system MUST treat
every session as sensitive by default, MUST make that status visible to the contributor, and
MUST NOT require a contributor's primary account.

The system MUST NOT transmit captured data anywhere without explicit, per-session, informed
action by the contributor.

**Rationale:** Contributors are volunteers accepting risk for a public good. Silent
exfiltration or an unflagged credential leak would betray that, and would end the project's
ability to recruit.

## Additional Constraints

**Settled architectural commitments.** Decided; not open for re-litigation without amendment:

- **Everything this project builds is written in Go.** Not "primarily Go" — the deliverable is a
  single binary and there is no second language in the build, the tests, or the shipped
  artifacts. No Python, no PowerShell scripts, no helper utilities in another runtime. Chosen for
  concurrent network I/O, single-binary distribution to non-expert contributors, and
  cross-compilation.
- **The target platform is Windows** (see v3.0.0). Star Conflict is a Windows title and its
  players are on Windows; the archive is bounded by how many of them can be recruited, so that is
  where the tool runs. Contributors are assumed to have a normal gaming machine and nothing else.
- **Platform-specific code is confined to acquisition, diagnosis and file protection.** Everything
  above them — framing, journalling, decoding, indexing, coverage, the bundle format — MUST be
  free of platform assumptions, so that a decoder written years from now can be pointed at an
  archive from any machine. A capture backend that cannot be built MUST degrade to a binary that
  still reads archives, never to one that refuses to run.
- **No scripts ship, in any language.** Anything a contributor needs to run is a `sccap`
  subcommand. This is the operative rule that keeps the language commitment honest: a bash
  helper is how a second language gets in.
- New capture tooling lives in `sc-capture/` within this repository and is self-contained: it
  MUST build from a fresh clone with no sibling checkout and no external module present. The
  Go module boundary, not a repository boundary, is what keeps it independently buildable.
- The new tooling **supersedes the Python proxy's capture role entirely**. Protocol knowledge
  that project recovered — dispatch tables, element names, Kaitai schemas, the framing and
  checksum reference — is public-domain prior art, snapshotted in `docs/protocol/`, to be
  honoured and verified rather than discarded or re-derived from scratch.
- Host-level system orchestration that is genuinely administrative work — driver installation, NIC
  offload control, service configuration — is **out of scope**. Reimplementing it in Go is a
  downgrade, and shipping it as a script would break the rule above. The system therefore
  **diagnoses the host rather than configuring it**: it MUST detect and report every condition
  that would silently compromise a capture, name the exact remedy, and refuse to pretend a
  misconfigured capture is a good one. Detection is in scope and mandatory; configuration is
  neither.
- Third-party tools used as **independent oracles** in verification — a separate packet capture
  to compare against, a disassembler, a hex editor — are not part of the project and are not
  bound by the language rule. Their independence is the point; reimplementing them here would
  destroy the value of the comparison.

**Known protocol context.** Implementations MAY assume the following, and MUST verify rather
than trust it:

- Length-prefixed framing over TCP with a 12-byte big-endian header and a checksum.
- Three TCP services: load balancer, shard, chat.
- Two container message types carry most traffic: an async request/response wrapper, and a
  notification wrapper carrying a self-describing variant map.
- A handoff message hands the client an address, port and session identifier for a dedicated
  match server reached over **UDP** — a protocol no existing tool has ever recorded.
- No transport-layer TLS on the master-server protocol; credential protection is
  application-layer.

## Development Workflow

- **Evidence before implementation.** A decoder MUST NOT be written for a message shape that has
  not been observed. Speculative decoding is how wrong assumptions enter an archive that
  outlives them.
- **Feasibility spikes precede design.** Where a capability depends on an unverified assumption
  about the game's behaviour, a minimal spike MUST establish feasibility before the capability
  is designed around. Spikes MUST run against non-competitive game modes.
- **Fault injection is a required test.** Principle II is only real if tested: the suite MUST
  include decoder failure, malformed frames and stream desync, and MUST assert that byte capture
  survives all of them.
- **Every change states its effect on the archive.** Contributions MUST declare whether they
  change what is captured, what is persisted, or the on-disk schema. Schema changes MUST be
  versioned and MUST remain readable by tools targeting prior versions.

## Governance

This constitution supersedes other practices where they conflict. Where a principle and a
convenience are in tension, the principle wins.

**Amendment.** Amendments require a written rationale, an explicit statement of what becomes
possible or impossible as a result, and a migration note for any already-archived sessions
affected. An amendment reducing what is captured carries the burden of proof.

**Versioning.** Semantic. MAJOR for removal or redefinition of a principle; MINOR for a new
principle or materially expanded guidance; PATCH for clarification that does not change meaning.

**Compliance.** Reviews MUST verify that a change introduces no capture-time filtering decision
(I), no path where a decoder can lose bytes (II), and no in-path behaviour that cannot be
disabled (IV). Complexity violating a principle must be justified in writing or removed.

### Amendment record

**v2.0.0 — 2026-08-13 — Principle VI redefined: "Shared With the Server" → "One Protocol
Implementation".**

*Rationale.* The sibling repositories were removed from this workspace. Principle VI as written
required sharing a protocol implementation with a server reimplementation that is no longer
present, and which could not in practice be imported anyway: its Go module declared the
unresolvable path `module starconflict`, and its parser was bound to a live `net.Conn` rather
than to a byte source. The principle stated a requirement nothing satisfied. Its intent — never
two divergent parsers of one protocol — is preserved and made enforceable within a single
codebase.

*What this makes possible.* Capture tooling builds from a fresh clone with no sibling checkout,
no `replace` directive, and no cross-repo release coordination. The licence is no longer forced
to EUPL-1.2 by an imported copyleft dependency. The protocol package can be designed for the
capture use case — incremental parsing from a buffer of already-journaled bytes — rather than
inheriting a server's socket-shaped interface.

*What this makes impossible.* There is no longer a mechanical guarantee that this tooling and any
future emulator agree on the wire format. Agreement now depends on that emulator choosing to
consume this package. The requirement that the protocol package stay free of capture, storage and
transport dependencies exists precisely to keep that choice available and cheap.

*Migration note for archived sessions.* None. No already-archived session is affected: the
on-disk bundle schema, the pcapng conventions, the record index and the element ids are all
unchanged. The `session.json` field `software.protocol_lib_commit`, which pinned the external
library, becomes the commit of the internal protocol package; bundles written before this
amendment remain readable and their field value remains meaningful as a reference to the external
repository at that commit.

**v2.1.0 — 2026-08-14 — Go-only build; host diagnosis replaces host orchestration.**

*Rationale.* "Implementation language is Go" described the main binary but left room for the
Python and shell helpers the earlier tooling relied on — a marker beacon, a bundle verifier, host
setup scripts. Those helpers were exactly where the previous generation of tooling fragmented,
and requiring a contributor to have a working Python environment before they can archive traffic
is friction that costs sessions. The language commitment is now absolute, and the operative rule
is that no scripts ship at all: anything a contributor runs is a `sccap` subcommand.

*What this makes possible.* One static binary with no runtime prerequisites is the whole install.
Marker stamping and bundle verification move into `sccap`, where they can see the session's own
state instead of guessing at it from the outside. Cross-compilation covers every contributor rig
without a per-platform scripting story.

*What this makes impossible.* The project can no longer ship host setup automation, because that
work is genuinely shell work and reimplementing it in Go is a downgrade. The replacement
obligation is stated in the constraints above and is stricter than what the scripts did:
the system must **detect and report** every condition that would silently compromise a capture —
wrong interface, offloads on, insufficient permissions, clock undisciplined — and must refuse to
present a compromised capture as a good one. Automating the fix was optional; noticing the
problem is not.

*Migration note for archived sessions.* None. No on-disk format, element id, or bundle
convention changes.

**v2.2.0 — 2026-08-14 — capture tooling lives in this repository rather than its own.**

*Rationale.* The separate-repository rule was inherited from a workspace that held three
independent checkouts with different licences and maintainers. That workspace no longer exists:
there is one project, one licence, and one set of contributors. A second repository for a
subdirectory of it bought nothing and cost real friction — two clones to get started, two commits
for one change, and a README that had to explain why the tool it documents was not in the
repository the reader had just cloned.

*What this makes possible.* One clone gets a contributor everything: the manual, the checklist,
the protocol reference and the tool that uses them. A change spanning the spec and the code is one
commit with one review. CI runs from the repository root over both.

*What this makes impossible.* The tool can no longer be depended on as a standalone repository,
and its history is no longer separable from the project's. The independence that actually matters
is preserved by the Go module boundary rather than a repository boundary — `sc-capture/` still
builds from a fresh clone with no external module, and `pkg/scproto` still depends on nothing else
(Principle VI). Should the tool ever need to ship separately, `git subtree split` recovers a
standalone history.

*Migration note for archived sessions.* None. No on-disk format, element id or bundle convention
changes.

**v2.3.0 — 2026-08-14 — Windows support, and the one commitment it costs.**

*Rationale.* Coverage is bounded by how many contributors can be recruited, and most Star Conflict
players are on Windows. A Linux-only tool caps the archive at the size of the Linux-playing
population, which is the wrong constraint to accept against a deadline.

*What this makes possible.* A Windows contributor can record, verify, decode and report coverage.
The on-disk bundle format, element ids and every guarantee attached to them are identical across
platforms, so captures from either are interchangeable at intake.

*What this costs.* The settled commitment to "a single static binary with no runtime
prerequisites" **no longer holds on Windows**. Live capture there requires Npcap, which requires
cgo, which forfeits static linking and cross-compilation — the three properties Go was chosen
for. Npcap's licence also forbids redistribution, so it cannot be bundled.

The cost is contained rather than accepted wholesale:

- The capture backend is behind a `npcap` build tag. A plain `go build` on Windows still produces
  a static binary with no prerequisites; it simply cannot record. Every offline command —
  `verify`, `decode`, `index`, `coverage` — works in that build, so the prerequisite is charged
  only to contributors who actually capture.
- `sccap doctor` reports a missing backend as a first-class check naming the install, rather than
  failing at capture time.
- On Linux nothing changes: still pure Go, still `CGO_ENABLED=0`, still one static binary.

*Contributor safety on Windows.* Principle VIII is not weakened. A file mode there means almost
nothing — `os.Chmod` toggles only the read-only attribute — so sessions get an explicit
owner-only DACL with inheritance severed, and verification reports the principals that hold
access rather than a mode string that would be a lie.

*Migration note for archived sessions.* None. No on-disk format, element id or bundle convention
changes.

**v3.0.0 — 2026-08-14 — Windows is the platform, not a second one.**

*Rationale.* v2.3.0 added Windows beside Linux and framed it as a concession that cost the
single-static-binary commitment. Holding both was the wrong reading of the deadline. Star Conflict
is a Windows title; its players are on Windows; the archive is bounded by how many of them can be
recruited, and every hour spent keeping a second platform working is an hour not spent recruiting
or recording. The Linux path also carried a hidden tax that only shows up under scrutiny: it was
the platform the manual was written for, so the entire onboarding document described a
Proton-based setup that most contributors would never use and that measurably degrades what a
capture is worth.

That last point is the substantive one, and it is about evidence rather than convenience. Proton
does not alter payload bytes, but everything beneath them is Linux's: the TCP stack, the timers,
the MTU, the retransmission behaviour. Principle V requires per-record timestamps precisely so
that tick rate, latency, keepalive cadence and retransmission can be answered later. Answering
them from a Proton capture answers them about Wine's stack, not about the game's. Capturing where
the game actually runs makes those recordings mean what they claim to mean.

*What this makes possible.* One platform, one manual, one setup path, and a contributor who is
already running the game can be recording within minutes of reading Part 2 — no compatibility
layer, no second machine, no gateway. Sessions carry transport-level behaviour that is the game's
own, which makes the timing questions answerable rather than merely recorded. `sccap doctor` can
give exact, actionable remedies instead of hedging across platforms.

*What this makes impossible.* The tool no longer builds or runs anywhere but Windows — not for
capture, and not for reading an archive either. Anyone wanting to analyse a bundle on another
system must either run the tool under Windows or write their own reader. That cost is real but
small and it is bounded: the bundle format is deliberately ordinary — standard `pcapng` segments
that Wireshark opens anywhere, plus JSON — so the evidence remains readable without this tool at
all. What is lost is convenience, not access, and Principle II is what guarantees that.

The single-static-binary commitment is likewise settled rather than mourned. Live capture requires
Npcap, which requires cgo, which forfeits static linking and cross-compilation; Npcap's licence
also forbids redistribution, so it cannot be bundled. This is now simply what the tool is, and the
cost is contained the same way it was in v2.3.0: the capture backend sits behind a `npcap` build
tag, a plain `go build` still produces a prerequisite-free binary that can verify, decode, index
and report coverage on any archived session, and `sccap doctor` reports a missing backend as a
first-class check naming the install rather than failing at capture time.

*Contributor safety.* Principle VIII is unchanged and its enforcement is now the only one that
exists: a file mode here means almost nothing — `os.Chmod` toggles the read-only attribute — so
sessions get an explicit owner-only DACL with inheritance severed, and verification reports the
principals that hold access rather than a mode string that would be a lie.

*Migration note for archived sessions.* None for the format: no on-disk schema, element id or
bundle convention changes, and a session recorded under the previous Linux build remains valid
and verifiable. Two recorded *values* change meaning and are worth knowing about when comparing
old sessions to new ones. `client.platform` now reads `windows` rather than `linux`.
`client.binary_build_id` now carries a PE identity rather than a GNU build-id, and a new
`client.binary_build_id_kind` field says which kind it is — `codeview` for a linker-stamped PDB
GUID and age, `image` for a TimeDateStamp and SizeOfImage pair. Neither is comparable with an ELF
build-id or with the other, which is exactly why the kind is recorded alongside the value.

**Version**: 3.0.0 | **Ratified**: 2026-08-13 | **Last Amended**: 2026-08-14
