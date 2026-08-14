# sccap — Star Conflict capture tooling

Archives the complete network footprint of a Star Conflict play session as a self-describing,
verifiable bundle, and tracks which parts of the protocol have never been observed.

One static Go binary. No interpreter, no libraries to install, no scripts.

## Build

```bash
CGO_ENABLED=0 go build -o out/sccap ./cmd/sccap
sudo setcap cap_net_raw,cap_net_admin=eip out/sccap
./out/sccap doctor
```

`doctor` must exit 0 before anything else is worth trying. It reports what is missing and the
exact command to fix it — it never changes host state itself.

Then, with the game running:

```bash
./out/sccap doctor --watch 30s
```

This reports which interfaces actually carry game traffic. Capturing on the wrong one produces a
session that passes every other check and contains nothing useful. It is the only failure mode
here that is both silent and total.

## Capture

```bash
./out/sccap capture --scenario AUTH-02 --region EU --out ./captures
# play, then Ctrl+C
./out/sccap verify ./captures/SC_*
```

Defaults are passive, unfiltered and full-snaplen. Nothing is rewritten and nothing is discarded.

## How it works

The tool copies every frame crossing the network interface into a standard `pcapng` file and
takes no part in the conversation. Decoding happens **afterwards**, reading the file — so a
decoder that crashes, desyncs or misreads cannot cost a single byte, because the byte was written
by a different path before the decoder ever saw it.

```
   game client ─────────────────────────────► game servers
                       │
                       │ (copy)
                       ▼
                 raw journal ──► decode / index / coverage
                  (evidence)        (derived, rebuildable)
```

Everything except the `pcapng` segments and `session.json` is derived data that can be thrown
away and regenerated.

## How you know nothing was missed

1. The game numbers its own messages — a gap in the sequence is visible
2. The transport numbers its own bytes, separately — a second gap detector
3. Every message carries a checksum — damage is detectable, not just absence
4. The kernel counts what it dropped, and that number is on screen during capture. It must be zero

## Commands

| Command | Purpose |
|---|---|
| `doctor` | Can this machine capture? What is missing? Which interface? |
| `capture` | Record a session |
| `mark` | Stamp a labelled moment onto the session timeline |
| `verify` | Confirm a bundle is complete and internally consistent |
| `index` | Rebuild the derived record index from the raw journal |
| `decode` | Read an archived session (works with no server reachable) |
| `coverage` | What protocol elements have never been observed? |
| `status` | Snapshot of a running capture |

### Reading an archive

```bash
./out/sccap decode ~/captures/SC_* --status unknown_element   # what surprised us
./out/sccap decode ~/captures/SC_* --type SCMD_CONNECT_DEDICATED_SERVER
./out/sccap coverage                                          # what is still missing
./out/sccap coverage --state never_observed                   # the list, in full
```

Filtering here is **read-time** filtering against a complete archive, which is
allowed. Capture-time filtering is not: the journal always holds everything.

### Improving a decoder later

```bash
./out/sccap index ~/captures/SC_* --rebuild
./out/sccap verify ~/captures/SC_* --write-sums
```

`--rebuild` regenerates `index.jsonl` from the pcapng segments alone and
verifies the raw journal's hashes are unchanged afterwards. Records that
previously read `undecoded` decode as decoders improve; the evidence never
changes. This works with every game service unreachable, which is the point.

### Coverage, and why "never observed" is the number that matters

404 protocol elements are known by name — 39 message types, 249 `AC_*` opcodes,
116 `SN_*` notifications — pulled from the game client's own string tables.
Each is in one of three states:

```
never_observed  →  observed_undecoded  →  decoded
```

State never regresses. **Only the first state has a deadline.** An element
captured but not understood is safe forever and can be decoded in ten years; an
element never captured dies with the servers. That is why breadth of capture
beats depth of decoding while the servers are up.

There is deliberately no `submit`, no `upload`, and no telemetry — the binary contains no egress
path at all. There is also no `prune`: the tool never deletes a session to reclaim space.

## Safety

Sessions contain authentication material — the master-server protocol has no transport
encryption. Every session is marked sensitive, created `0700`/`0600`, and nothing leaves your
machine unless you send it yourself. Use a throwaway game account.

## Licence

MIT, see `LICENSE.txt`. Protocol element tables are public-domain in origin; see
`docs/protocol/PROVENANCE.md` in the parent workspace.
