# Quickstart & Validation: Comprehensive Protocol Capture Proxy

**Feature**: `001-capture-proxy` | **Date**: 2026-08-13

How to build, run and — more importantly — **prove** this feature works. Each scenario below maps
to acceptance criteria in [spec.md](./spec.md) and is written so it can be run by someone who was
not involved in building it.

Command surface: [contracts/cli.md](./contracts/cli.md). On-disk format:
[contracts/session-bundle.md](./contracts/session-bundle.md).

---

## Prerequisites

| Requirement | Check | Notes |
|---|---|---|
| Go 1.26+ | `go version` | Present: go1.26.5 |
| Protocol tables | `ls ../docs/protocol/*.json` | 404 elements, embedded at build time (research.md R6/R7) |
| Packet capture capability | `getcap $(which sccap)` | `cap_net_admin,cap_net_raw=eip` |
| Game client | reaches the hangar | Ubuntu + Proton per the capture manual |
| `tshark` / `dumpcap` | `tshark -v` | **Independent oracles only** — used to check our work in Scenario 2, never required to capture. Not currently installed: `sudo apt install tshark` |

Nothing in the runtime path is non-Go: `sccap` is one static binary with no interpreter, library
or script dependencies. The third-party capture tools above are deliberately outside that rule —
Scenario 2 compares our output against a tool we did not write, and reimplementing it here would
destroy the independence that makes the comparison meaningful.

Host preparation — package installation, NIC offload control, network namespace setup — is
**out of scope for this feature** per the constitution and stays shell work. Use the procedures
in `docs/Star-Conflict-Capture-Protocol.md` §1.

> **Note**: the `tools/` directory referenced by that manual and by the root `README.md` is not
> present in this workspace (see research.md R14). `sccap doctor` degrades to diagnosing and
> reporting rather than fixing, so nothing here depends on those scripts.

## Build

```bash
cd sc-capture
CGO_ENABLED=0 go build -o out/sccap ./cmd/sccap
sudo setcap cap_net_raw,cap_net_admin=eip out/sccap
./out/sccap doctor
```

The module is self-contained: it builds from a fresh clone with no external module, one
dependency (`gopacket`), and no cgo.

**Protocol parity check** — run before trusting any decode output:

```bash
go test ./pkg/scproto/... -run Golden -v
```

Asserts framing and checksum agreement against vectors generated from the archived reference
implementation at `docs/protocol/source/protocol.py`. The case that matters most is the
big-endian wire header hashed in little-endian order (research.md R6).

`doctor` must exit `0` before anything else is worth trying. It names the exact missing
capability and the command that fixes it, and it never changes host state itself — host setup is
out of scope, and no scripts ship in any language.

**Then confirm you are watching the right wire**, with the game running:

```bash
sccap doctor --watch 30s
```

This reports which interfaces actually carry traffic to game endpoints. Capturing on the wrong
one produces a session that passes every other check in this document — zero drops, valid bundle,
clean verification — and contains none of the game's traffic. It is the only failure mode here
that is both silent and total, so it is worth the 30 seconds every time your network setup
changes.

---

## Scenario 1 — First capture, verified end to end

**Proves**: US1 acceptance 1–3 · FR-001, FR-025, FR-026, FR-028 · SC-001, SC-011

```bash
sccap capture --scenario AUTH-02 --region EU --out ./captures
# play a login-to-hangar session, then Ctrl+C
sccap verify ./captures/SC_*__AUTH-02__*
```

**Expected**: a bundle directory containing at least one `capture_*.pcapng`, a `session.json`, and
`SHA256SUMS`. `verify` exits `0`. The progress line showed `drops=0` throughout.

**Also assert** — a reader who was not present can recover, from `session.json` alone: schema
version, `sccap` version, client build, UTC start/end, clock discipline, and which services were
observed. If any of those requires asking the person who captured it, SC-011 has failed.

**Time budget**: from `go build` to a verified bundle in under 15 minutes for someone who has
never read protocol documentation (SC-001).

---

## Scenario 2 — Byte-exactness against an independent capture

**Proves**: US1 acceptance 2 · FR-001, FR-004 · SC-002. **The single most important test here.**

Run `sccap` and `dumpcap` against the same interface simultaneously, then compare.

```bash
dumpcap -i <if> -n -s 0 -w /tmp/independent.pcapng &
sccap capture --scenario BASE-01 --interface <if> --out ./captures
# play for ~2 minutes, stop both
```

Compare the two frame streams — timestamps will differ in the low nanoseconds, so compare packet
bytes, not whole files:

```bash
tshark -r /tmp/independent.pcapng -T fields -e data.data > /tmp/a.txt
tshark -r ./captures/SC_*__BASE-01__*/capture_00001_*.pcapng -T fields -e data.data > /tmp/b.txt
diff /tmp/a.txt /tmp/b.txt && echo "BYTE-IDENTICAL"
```

**Expected**: no differences. **A single dropped or truncated frame fails this scenario**, and a
failure here invalidates every other result — nothing downstream matters if the journal is not
complete.

**Also assert**: transport headers are present and intact — TCP sequence numbers, window sizes,
checksums, and any retransmissions are visible in both captures. This is what a proxy-based
journal structurally cannot provide (research.md R2), so it is worth checking explicitly.

---

## Scenario 3 — Fault injection: the decoder cannot lose bytes

**Proves**: FR-016, FR-017 · SC-003 · Constitution Principle II. **Required by the constitution's
development workflow, not optional.**

Runs offline against a checked-in corpus — no game, no server, no network.

```bash
go test ./tests/faultinjection/... -v
```

The corpus injects, per case: a decoder panic, a malformed frame, a mid-stream desync, an unknown
message type, and an unknown `AC_*` opcode.

**Expected**, for every case:

| Assertion | Why |
|---|---|
| pcapng bytes identical to the no-fault control run | Byte loss is zero |
| Session not terminated | A decoder failure is not a capture failure |
| Relay (when enabled) not interrupted | Decoder is not in the traffic path |
| The offending record present in `index.jsonl` with `status: failed` or `unknown_element` | The record that caused it is not the record that vanishes |
| Desync point recorded in `session.json.connections[].desync_at_frame` | Degradation is observable, not silent |

**Structural backstop**: an import-cycle test asserting `internal/journal` does not import
`internal/decode`. If that test ever fails, Principle II has been violated architecturally and no
amount of careful coding restores it.

---

## Scenario 4 — Abrupt termination leaves a valid session

**Proves**: US1 acceptance 4 · FR-006 · SC-008

```bash
sccap capture --scenario BASE-01 --out ./captures &
sleep 30
kill -9 %1                       # SIGKILL — no clean shutdown path runs
sccap verify ./captures/SC_*__BASE-01__*
```

**Expected**: `verify` reports status `interrupted` and exits `0`. The pcapng walks structurally
to the truncation point. At most ~1 second of records is missing (the flush cadence, research.md
R9). `index.jsonl`'s final line may be truncated; that is detected and does not fail the bundle.
`session.json` has no `utc_end` and `termination: interrupted`.

Repeat 20 times. SC-008 requires **100%** of trials to produce a valid, verifiable session.

---

## Scenario 5 — In-match traffic reaches the journal

**Proves**: US2 acceptance 1, 2, 4 · FR-011, FR-012 · SC-005

Passive capture — no relay, no rewriting, no ban exposure. Use a **non-competitive mode** only.

```bash
sccap capture --scenario CBT-07 --region EU --out ./captures
# enter a non-competitive match, play it out, return to hangar, Ctrl+C
sccap decode ./captures/SC_*__CBT-07__* --type SCMD_CONNECT_DEDICATED_SERVER --json
```

**Expected**: the handoff record decodes, yielding `addr`, `port`, `session_id` and `zone_id`.
Then:

```bash
sccap decode ./captures/SC_*__CBT-07__* --conn <dedicated-conn-id> --json | head
```

**Expected**: UDP records in **both** directions, with wall and monotonic timestamps, spanning the
match. `session.json.services_observed` includes `dedicated`, and the connection's
`service_evidence` is `handoff` — meaning the flow was identified from the protocol, not guessed
from a port.

This is traffic no tool in this project has ever recorded. Getting it once is the highest-value
outcome in the feature.

---

## Scenario 6 — Coverage shrinks as the game is exercised

**Proves**: US3 acceptance 1–4 · FR-020–024 · SC-004

```bash
sccap coverage --json > /tmp/before.json
sccap capture --scenario ECON-03 --out ./captures     # exercise the store
sccap coverage --json > /tmp/after.json
```

**Expected**: the never-observed set shrinks by exactly the elements that appeared, and by
nothing else. Elements observed but not decodable report `observed_undecoded`, distinctly from
`never_observed` (FR-022). Coverage persists across restarts and aggregates across every session
on the machine.

```bash
sccap coverage --state never_observed | wc -l      # baseline ~232 of 404 known
```

**Also assert — state never regresses.** Capture a session that fails to decode an element that a
previous session decoded, then re-check: it must still read `decoded`. A regression here would
make the project's progress metric untrustworthy, which is worse than not having one.

---

## Scenario 7 — Offline re-decode, years later

**Proves**: US5 acceptance 1–3 · FR-029, FR-030 · SC-007

Run with every game service unreachable — pull the network cable, or run in an empty namespace.

```bash
sha256sum ./captures/SC_*__AUTH-02__*/capture_*.pcapng > /tmp/raw-before.txt

sccap decode ./captures/SC_*__AUTH-02__* --json > /tmp/decode-1.json
sccap index ./captures/SC_*__AUTH-02__* --rebuild
sccap decode ./captures/SC_*__AUTH-02__* --json > /tmp/decode-2.json

diff /tmp/decode-1.json /tmp/decode-2.json && echo "REPRODUCIBLE"
sha256sum -c /tmp/raw-before.txt && echo "RAW UNMODIFIED"
```

**Expected**: decoding completes with no server reachable; results are identical to those recorded
at capture time; the raw journal's hashes are unchanged by the rebuild.

**Then extend a decoder** and rebuild again: records that previously read `undecoded` now read
`decoded`, and the pcapng hashes are *still* unchanged. That is the whole point of the
architecture — new understanding applied to old evidence, without touching the evidence.

**Version refusal**: hand-edit a copy's `schema_version` to `99.0` and re-run `verify`. It must
exit `5` with a diagnostic naming both versions, and must not attempt a partial read (FR-027).

---

## Scenario 8 — Nothing leaves the machine

**Proves**: US6 acceptance 1–3 · FR-031, FR-032 · SC-009

```bash
sudo tcpdump -i any -n 'not host <game-server-ips>' -w /tmp/egress.pcap &
sccap capture --scenario BASE-01 --out ./captures
# full session, then stop both
tshark -r /tmp/egress.pcap -Y 'tcp.flags.syn==1 && tcp.flags.ack==0'
```

**Expected**: no outbound connection to anything other than the game's own servers. The stronger
form of this assertion is by inspection rather than observation: the binary contains **no
submission or telemetry code path at all** (contracts/cli.md, "Subcommands deliberately absent"),
so there is nothing that could fire.

**Also assert**:

```bash
stat -c '%a %n' ./captures/SC_*__BASE-01__* ./captures/SC_*__BASE-01__*/*
```

Directory `700`, files `600`. `session.json` has `sensitive: true` with a stated reason, and
`credential_warning: true` if authentication traffic was observed — set without the contributor
having to know it should be.

---

## Scenario 9 — Passive is the default, and it works alone

**Proves**: US6 acceptance 3 · FR-012, FR-013 · Principle IV

```bash
sccap capture --scenario BASE-01 --out ./captures        # no flags — passive
sccap verify ./captures/SC_*__BASE-01__* --json | grep -E '"mode"|"rewrites"'
```

**Expected**: `passive []` — mode is passive and the rewrite list is empty, without the
contributor having asked for it. The session is fully valid and verifiable. No traffic was
rewritten.

---

## Scenario 10 — Disk floor stops cleanly

**Proves**: FR-036, FR-037 · edge case "disk exhaustion"

```bash
# small tmpfs to make the floor reachable in seconds
sudo mount -t tmpfs -o size=600M tmpfs /mnt/tiny
sccap capture --scenario BASE-01 --out /mnt/tiny --min-free 400MiB --floor 200MiB
```

**Expected**: repeated visible warnings after the threshold; on reaching the floor, capture stops
with exit `4`. The session closes cleanly — `session.json` complete with
`termination: disk_floor`, and `SHA256SUMS` written. `verify` exits `0`. **No prior session was
deleted or overwritten to reclaim space.**

---

## Scenario 11 — Relay feasibility spike (time-boxed)

**Proves**: US2 acceptance 3 · FR-008–010, FR-014 · SC-006

This is a **spike, and disproving it is a valid and useful outcome** — passive capture already
archives in-match traffic, so nothing depends on this succeeding. Non-competitive modes only.

```bash
sccap capture --scenario CBT-07 --relay --relay-udp --out ./captures
```

**Expected, either way**: the attempt, any rejection, and all traffic leading to it are recorded,
and the contributor is told plainly whether the relay is viable rather than being left with a
broken client. `session.json.rewrites` enumerates every rewrite performed.

If the match **is** joinable through the relay, measure the cost:

```bash
sccap decode ./captures/SC_*__CBT-07__* --conn <dedicated-conn-id> --json \
  | <compute p99 RTT delta vs. an unrelayed baseline session>
```

**Expected**: ≤ 15 ms added round-trip at p99 (SC-006). Above that, the relay is not viable for
gameplay regardless of whether the connection is accepted, and passive capture remains the
supported path.

---

## Coverage of acceptance criteria

| Scenario | Spec coverage |
|---|---|
| 1 | US1 · FR-001, 025, 026, 028 · SC-001, SC-011 |
| 2 | US1 · FR-001, 004 · SC-002 |
| 3 | FR-005, 016, 017 · SC-003 · Principle II |
| 4 | US1 · FR-006 · SC-008 |
| 5 | US2 · FR-011, 012 · SC-005 |
| 6 | US3 · FR-020–024 · SC-004 |
| 7 | US5 · FR-002, 027, 029, 030 · SC-007 |
| 8 | US6 · FR-031, 032, 033 · SC-009 |
| 9 | US6 · FR-012, 013 |
| 10 | FR-036, 037 |
| 11 | US2 · FR-008, 009, 010, 014 · SC-006 |

**Not covered by a scenario above**: SC-010 and SC-012, both of which need a second person.
SC-010 — have one contributor capture a session performing a known action sequence with no notes,
and a second reconstruct it from `sccap decode` output alone. SC-012 — open a bundle's pcapng in
Wireshark on a machine with none of this project's software installed.
