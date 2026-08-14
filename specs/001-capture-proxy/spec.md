# Feature Specification: Comprehensive Protocol Capture Proxy

**Feature Branch**: `001-capture-proxy`

**Created**: 2026-08-13

**Status**: Draft

**Input**: User description: "Replicate the sc-proxy approach but have it capture EVERYTHING we need — a full blown capture proxy that is smart enough to know when to capture what, superseding the existing Python proxy's capture role."

## Clarification of Intent

The phrase "smart enough to know when to capture what" is interpreted here as **smart about what
it tells the operator, not about what it keeps**. Constitution Principle I forbids capture-time
filtering: selective capture requires knowing what matters, and in a preservation project that
is not knowable until after the servers are gone. "Smart" therefore means the system knows what
it is looking at, knows what is novel, and knows what it has never seen — while still keeping
everything. This reading is carried throughout the requirements below.

## Clarifications

### Session 2026-08-13

- Q: Should this feature stop at producing verified sessions on the contributor's local disk, with getting those sessions to the project handled entirely separately? → A: Out of scope. Feature ends at a verified session on local disk; sharing is manual and a separate feature.
- Q: In what form must the raw byte journal be stored so that someone can still read it years from now, once this project and its authors are gone? → A: A standard packet-capture container for the bytes, plus a documented sidecar index carrying project-specific per-record metadata (service identity, direction, monotonic clock, decode result).
- Q: How much extra round-trip delay is the relay allowed to add before it counts as a failure? → A: ≤ 15 ms added round-trip at the 99th percentile, measured against an unrelayed baseline on the same machine, with the blind-comparison test retained as a secondary check.
- Q: What should the program do when the contributor's disk is filling up during a live capture? → A: Unbounded retention. Warn at a configurable free-space threshold; on reaching the floor, close the session cleanly (valid and verifiable) and stop capturing rather than write a partial record.
- Q: Should captured sessions be protected on the contributor's own disk, given they contain login credentials in the clear? → A: No encryption at rest. Owner-only file permissions on the session directory, plus the existing sensitivity marking and credential warning.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Record a complete play session, byte-exact and verifiable (Priority: P1)

A contributor downloads a single program, starts it, plays the game normally, and stops it. They
are left with a self-contained session that provably contains every byte exchanged with the game
services in both directions, timestamped, with enough metadata for someone who was not present
to interpret it years later.

**Why this priority**: This is the archive. Every other capability is an enrichment of it, and
without it nothing else has anything to operate on. It also closes the largest fidelity gap in
the current tooling, which stores only decoded message bodies with no per-record timestamps and
no transport headers — making tick rate, latency, keepalive cadence and retransmission
permanently unanswerable from existing captures.

**Independent Test**: Run the program, play a login-to-hangar session, stop it. Independently
capture the same traffic with a general-purpose packet analyser and assert the two byte streams
are identical. Confirm the session verifies its own integrity with no external input.

**Acceptance Scenarios**:

1. **Given** a contributor with no protocol knowledge, **When** they run the program and play a
   session, **Then** a single verifiable session artifact is produced without further action.
2. **Given** a completed session, **When** its raw journal is compared byte-for-byte against an
   independent capture of the same traffic, **Then** the two are identical.
3. **Given** a completed session, **When** it is inspected by someone who was not present,
   **Then** the schema version, software version, client build, UTC time range, clock discipline
   and interposed connections are all discoverable from the session itself.
4. **Given** a running session, **When** the program is terminated abruptly, **Then** the
   partial session is still valid, self-describing, and verifiable up to the point of failure.

---

### User Story 2 - Reach the in-match traffic nothing has ever recorded (Priority: P1)

A contributor enters a match through the proxy. The realtime gameplay traffic — the protocol
governing ship movement, weapons, damage and entity replication — is journaled for the first
time, in both directions, alongside the master-server traffic that preceded it.

**Why this priority**: This is the only genuinely irreplaceable gap. The master-server protocol
is partly decoded and can be re-examined from existing captures; the in-match protocol has never
been recorded by any tool in the project and disappears entirely at shutdown. It is also the
only capability whose feasibility is unproven, so establishing it early determines the shape of
everything downstream.

**Independent Test**: Enter a non-competitive match through the proxy. Assert the match is
playable and that datagrams are journaled in both directions with timestamps. If the match
cannot be joined through the relay, the capability is disproven — an equally valuable and much
cheaper outcome than discovering it late.

**Acceptance Scenarios**:

1. **Given** a contributor connected through the proxy, **When** the server hands off to a
   dedicated match server, **Then** the handoff is recorded and the subsequent realtime traffic
   is journaled in both directions.
2. **Given** an in-progress match through the relay, **When** the contributor plays normally,
   **Then** added round-trip time stays within the budget in SC-006 and no traffic is dropped
   from the journal.
3. **Given** a relayed match connection that the game rejects, **When** the rejection occurs,
   **Then** the attempt, the rejection and all traffic leading to it are recorded, and the
   contributor is told the relay is not viable rather than being left with a broken client.
4. **Given** the relay is disabled, **When** a contributor plays a match, **Then** the
   master-server traffic is still captured in full and the session remains valid.

---

### User Story 3 - Know what has never been observed (Priority: P2)

A contributor asks the system what is still missing and receives a concrete list of protocol
elements that have never appeared in any session they have captured. They go and exercise those
parts of the game, and the list shrinks.

**Why this priority**: This converts an open-ended activity into a closed loop with a completion
condition, which is what makes distributed volunteer effort efficient against a deadline. It
depends on User Story 1 existing but delivers value the moment it does.

**Independent Test**: Capture two sessions exercising different parts of the game. Assert the
never-observed set shrinks by exactly the elements that appeared, persists across restarts, and
correctly aggregates across sessions.

**Acceptance Scenarios**:

1. **Given** any number of completed sessions, **When** the contributor requests coverage,
   **Then** the system reports which known protocol elements have never been observed.
2. **Given** a new session containing a previously unseen element, **When** the session
   completes, **Then** that element is removed from the never-observed set permanently.
3. **Given** an element that has been observed but cannot be decoded, **When** coverage is
   reported, **Then** it is distinguished from an element never observed at all.
4. **Given** traffic containing an element unknown to the system entirely, **When** it is
   observed, **Then** it is recorded, flagged as novel, and surfaced to the contributor.

---

### User Story 4 - Understand a session without having been there (Priority: P2)

Someone opens an archived session and can see what happened — which messages were exchanged,
what they were called, and what the player was doing — without having been present and without
the contributor having narrated anything.

**Why this priority**: Sessions arrive from distributed contributors and are read by people who
were not there. Because the system already decodes the traffic, it can label sessions itself,
removing an entire class of manual annotation work and the errors that come with it.

**Independent Test**: Have one person capture a session performing a known sequence of in-game
actions without recording any notes. Have a second person read the session and reconstruct the
sequence.

**Acceptance Scenarios**:

1. **Given** a session containing recognised traffic, **When** it is read, **Then** each record
   is labelled with its message type and, where applicable, its sub-type name.
2. **Given** a session containing an economy transaction, **When** it is read, **Then** the
   transaction is identifiable without external notes.
3. **Given** a record the system cannot decode, **When** it is read, **Then** it is presented as
   undecoded raw bytes rather than omitted or silently misrepresented.
4. **Given** a contributor performing an action the system cannot infer, **When** they mark it
   during capture, **Then** the mark is recorded in the session timeline against the traffic.

---

### User Story 5 - Re-decode the archive after the servers are gone (Priority: P3)

Long after shutdown, someone improves a decoder and applies it to sessions captured years
earlier, extracting meaning that was not understood at capture time.

**Why this priority**: This is the payoff for Constitution Principle II and the reason raw
journaling is non-negotiable. It requires no live server, so it is the one capability with no
deadline — but it is only possible if the earlier stories were built correctly.

**Independent Test**: With all game services unreachable, run decoding over an archived session
and obtain results identical to those produced at capture time. Then extend a decoder and
confirm previously undecoded records now decode, with no change to the raw journal.

**Acceptance Scenarios**:

1. **Given** an archived session and no reachable server, **When** decoding is run over it,
   **Then** it completes and reproduces the labels recorded at capture time.
2. **Given** an improved decoder, **When** it is applied to an archived session, **Then**
   additional records decode and the raw journal is unmodified.
3. **Given** a session written by an older schema version, **When** current tooling reads it,
   **Then** it is read successfully or refused with an explicit version diagnostic — never
   misread.

---

### User Story 6 - Contribute without exposing yourself (Priority: P3)

A contributor understands what their session contains, that it is sensitive, and that nothing
leaves their machine unless they choose to send it.

**Why this priority**: Sessions contain authentication material. Contributor trust is a
precondition for recruitment, and recruitment is a precondition for coverage.

**Independent Test**: Complete a session while monitoring all network egress. Assert no captured
data leaves the machine. Assert the session is marked sensitive without the contributor having
to know it should be.

**Acceptance Scenarios**:

1. **Given** a completed session, **When** the contributor inspects it, **Then** it is marked
   sensitive by default, the reason is stated, and its files are accessible only to the
   contributor's own account.
2. **Given** a session on disk, **When** no explicit submission action is taken, **Then** no
   captured data is transmitted anywhere.
3. **Given** a contributor who wants no in-path modification, **When** they select passive
   operation, **Then** no traffic is rewritten and capture still produces a valid session.

---

### Edge Cases

- **The relay is refused.** The match server rejects a connection arriving via the relay —
  for example because the session identifier is bound to the address the master server
  registered. The attempt must be fully recorded, the contributor told plainly, and the
  master-server capture unaffected.
- **The handoff never arrives.** The client reaches a match by a path that does not use the
  observed handoff message. Traffic to an unanticipated endpoint must still be recognised as
  in-scope rather than silently ignored.
- **Stream desync.** Framing assumptions fail mid-session. Raw capture must continue unbroken
  while decoding degrades to undecoded, and the desync point must be recorded.
- **Decoder panic on malformed input.** Must not terminate the session, break a relay, or lose
  the record that caused it.
- **Upstream unreachable mid-session.** Connection loss, timeout and the client's retry
  behaviour are themselves valuable observations and must be captured, not treated as an error
  to abort on.
- **Client reconnects mid-session.** A new connection must join the existing session rather than
  starting a disconnected one.
- **Abrupt termination.** Power loss or kill signal must leave a session that is valid and
  verifiable up to the failure point.
- **Disk exhaustion.** Must be warned about ahead of the floor, and on reaching it must end in a
  cleanly closed, verifiable session with capture stopped — never in silently discarded traffic,
  a partial record, or the removal of prior sessions to make room.
- **Wall-clock discontinuity.** A clock step mid-session must not corrupt ordering; monotonic
  ordering must survive it.
- **Traffic on an interposed port that is not the expected protocol.** Must be journaled as
  unknown rather than dropped or forced into a parse.

## Requirements *(mandatory)*

### Functional Requirements

**Capture and persistence**

- **FR-001**: System MUST persist a byte-exact record of every byte observed on every interposed
  connection, in both directions, without filtering, sampling or truncation.
- **FR-002**: System MUST persist raw bytes independently of any decoding, such that all decoded
  output is reproducible from the raw record alone. Raw bytes MUST be written to a standard
  packet-capture container readable by general-purpose third-party tooling, so the journal
  remains readable without any software produced by this project.
- **FR-003**: System MUST record, for every record, a wall-clock UTC timestamp and a monotonic
  timestamp.
- **FR-004**: System MUST persist complete transport framing, including all header fields and
  checksums, not only message payloads.
- **FR-005**: System MUST journal traffic it cannot parse, cannot name, or does not expect, and
  MUST mark it as such rather than omitting it.
- **FR-006**: System MUST write raw records durably as capture proceeds, such that an abrupt
  termination loses at most the records in flight.
- **FR-007**: System MUST record which connection each record belongs to and in which direction
  it travelled. Per-record metadata that the standard container cannot express — logical service
  identity, direction, monotonic timestamp, decode result — MUST be carried in a documented
  sidecar index keyed to the records in the container, and that index format MUST be published
  alongside the archive.

**Interposition**

- **FR-008**: System MUST interpose on every service the client uses to reach the game,
  including the load balancer, shard, chat, the client's web service calls, and the dedicated
  match server.
- **FR-009**: System MUST relay all traffic byte-for-byte, except for an explicitly enumerated
  set of address rewrites required to route connections through itself.
- **FR-010**: System MUST document every rewrite it performs and record in the session which
  rewrites were active.
- **FR-011**: System MUST detect the handoff to a dedicated match server, record it, and
  interpose on the resulting connection.
- **FR-012**: System MUST continue to produce a valid session when interposition on any one
  service fails or is disabled.
- **FR-013**: System MUST offer an operating mode that performs no rewriting at all, and MUST
  produce a valid session in that mode.
- **FR-014**: System MUST NOT fuzz, replay, inject or synthesise traffic toward live services.

**Decoding and annotation**

- **FR-015**: System MUST label recognised records with their message type and sub-type names.
- **FR-016**: System MUST isolate decoding failures such that no failure causes byte loss,
  session termination, or relay interruption.
- **FR-017**: System MUST present records it cannot decode as undecoded raw bytes, never as a
  partial or speculative interpretation presented as complete.
- **FR-018**: System MUST allow a contributor to mark a moment during capture with a free-text
  label recorded against the traffic timeline.
- **FR-019**: System MUST derive session annotations from decoded traffic where possible, so a
  session is interpretable without contributor narration.

**Coverage**

- **FR-020**: System MUST maintain, persistently and across sessions, the set of known protocol
  elements that have never been observed.
- **FR-021**: System MUST report that set on demand.
- **FR-022**: System MUST distinguish never observed, observed but not decodable, and decoded.
- **FR-023**: System MUST flag elements that are entirely unknown to it when first observed.
- **FR-024**: Coverage state MUST survive restarts and MUST aggregate across all sessions on the
  machine.

**Session integrity and metadata**

- **FR-025**: System MUST record per session: schema version, software version, client build,
  UTC start and end, clock discipline, interposed services, and active rewrites.
- **FR-026**: System MUST produce integrity hashes covering every file in a session.
- **FR-027**: System MUST version the on-disk schema, and readers MUST either read prior
  versions or refuse them with an explicit diagnostic.
- **FR-028**: System MUST provide a verification action that a contributor can run to confirm a
  session is complete and internally consistent before sharing it.

**Offline operation**

- **FR-029**: System MUST decode archived sessions with no game service reachable.
- **FR-030**: System MUST allow an improved decoder to be applied to previously archived
  sessions without modifying their raw records.

**Safety and privacy**

- **FR-031**: System MUST mark every session sensitive by default and state why. Session files
  MUST be created with owner-only access permissions. Sessions MUST NOT be encrypted at rest, so
  that the raw journal stays directly re-parsable by future tooling with no key required and no
  key-loss path that could destroy an archive.
- **FR-032**: System MUST NOT transmit captured data anywhere without explicit per-session
  action by the contributor.
- **FR-033**: System MUST warn a contributor when a session is likely to contain credential
  material.

**Operability**

- **FR-034**: System MUST be usable by a contributor with no protocol knowledge, requiring no
  configuration beyond pointing the game at it.
- **FR-035**: System MUST report during capture that it is working — records captured, services
  interposed, and anything novel seen.
- **FR-036**: System MUST retain sessions until explicitly removed, and MUST NOT delete or
  overwrite prior sessions to reclaim space.
- **FR-037**: System MUST retain sessions without any size or total-volume budget, and MUST warn
  the contributor when free storage falls below a configurable threshold. On reaching a hard
  free-space floor, the system MUST close the current session cleanly — leaving it valid and
  verifiable — and stop capturing, rather than write a partial record, discard traffic, or
  remove prior sessions.

### Key Entities

- **Session**: One continuous capture run. Owns a raw journal — a standard packet-capture
  container paired with its documented sidecar index — plus metadata, integrity hashes,
  annotations, and a time range. The unit of verification and sharing.
- **Record**: One observed unit of traffic — a datagram, or a framed message on a stream. Carries
  raw bytes, both timestamps, connection identity, direction, and an optional decode result.
- **Connection**: One interposed path between client and an upstream service, identified by the
  service it represents, its endpoints, and whether it was rewritten.
- **Decode Result**: The derived interpretation of a record — message type, sub-type, decoded
  fields, or an explicit failure reason. Always reproducible from the record; never a substitute
  for it.
- **Coverage State**: Persistent, cross-session record of which known protocol elements have been
  observed, which decoded, and which never seen. The project's progress metric against the
  deadline.
- **Annotation**: A labelled point or span on a session timeline, either contributor-supplied or
  derived from decoded traffic.
- **Protocol Element**: A named, addressable unit of the protocol whose observation is tracked —
  a message type, an async-request opcode, or a notification type.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A contributor with no protocol knowledge goes from first launch to a verified
  session in under 15 minutes, without reading protocol documentation.
- **SC-002**: 100% of bytes observed on interposed connections are recoverable byte-identically
  from the raw journal, verified against an independent packet capture of the same traffic.
- **SC-003**: Under fault injection — decoder panic, malformed frame, stream desync, unknown
  message type — byte loss is zero and no session is terminated.
- **SC-004**: The system reports the never-observed protocol element set at any time, and that
  set is correct against a manually verified sample of sessions.
- **SC-005**: In-match traffic is journaled in both directions for a complete match, or the
  attempt is definitively recorded as infeasible with the evidence to show why.
- **SC-006**: Relaying adds no more than 15 ms to round-trip time at the 99th percentile,
  measured against an unrelayed baseline on the same machine. As a secondary check, contributors
  report no difference in a blind comparison against unrelayed play.
- **SC-007**: An archived session decodes fully with all game services unreachable, producing
  results identical to those recorded at capture time.
- **SC-008**: A session interrupted by abrupt process termination remains valid and verifiable
  up to the interruption point in 100% of trials.
- **SC-009**: No captured data leaves the contributor's machine without an explicit submission
  action, verified by monitoring all network egress across a full session.
- **SC-010**: A reader who was not present can reconstruct the sequence of in-game actions from
  an archived session alone, without contributor notes.
- **SC-011**: Every session is self-describing: schema version, software version, client build
  and time range are recoverable from the session with no external reference.
- **SC-012**: A raw journal opens and displays correctly in general-purpose third-party packet
  analysis tooling on a machine with none of this project's software installed.

## Assumptions

- **The dedicated match server is reachable through a relay.** Unproven. The handoff message
  carries an address, port and session identifier; if that identifier is bound to the address
  the master server registered, a relayed connection may be refused. User Story 2 is written to
  deliver value either way — disproving it early is a valid and useful outcome — but if it is
  refused, the in-match protocol may be unreachable by any proxy-based approach, and passive
  wire-level capture becomes the only route to it.
- **Contributors run the game and the proxy on the same machine**, and can point the client at a
  local endpoint. Contributors who cannot do this remain able to contribute passively.
- **The protocol knowledge already recovered by the earlier Python project is broadly correct**
  — framing, message type table, opcode names — and is available as public-domain prior art. A
  frozen snapshot is held in `docs/protocol/` (404 named elements). It is treated as a starting
  hypothesis to verify, not as ground truth.
- **The system is self-contained.** It builds and runs with no external module present, and
  holds exactly one implementation of framing, message typing and value encodings, per
  Constitution Principle VI as amended on 2026-08-13.
- **Contributors use throwaway game accounts.** The system marks sessions sensitive regardless,
  but the safety story assumes primary accounts are not being risked.
- **Storage is not the binding constraint.** Unfiltered capture is assumed affordable; FR-037
  exists to detect the case where it is not.
- **Non-competitive game modes are available** for relay testing, so feasibility work never
  degrades other players' matches.
- **Submission and distribution are out of scope for this feature.** Confirmed: the system
  produces verified sessions on local disk and stops there. Sharing is a manual contributor
  action performed by whatever means they choose, and any automated submission channel is a
  separate feature with its own trust and privacy design.
