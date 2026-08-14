# Capture Phases — what is done, what is next

The tool works and the machine is ready. Nothing has been recorded yet.

This document is the **running order and the scoreboard**: nine phases, each with a
plain-English goal, the exact commands to run, and a finish line you can *check* rather
than guess at. Work down it in order. When you come back after two weeks away, the
[position check](#where-am-i-right-now) tells you in ten seconds where you stopped.

- [The README](../README.md) is the how-to — build, permissions, the recording envelope.
- [The capture manual](Star-Conflict-Capture-Protocol.md) is the detail for each individual scenario.
- **This document is the order and the progress.** It repeats neither of the other two.

---

## Where am I right now?

**As of 2026-08-14:**

```
Machine ready ............. yes    sccap doctor: all OK, client detected, offloads off
Bundles recorded .......... 0      packet-caps/ is empty
Elements never observed ... 404    of 404 known
Current phase ............. 1      "Prove the chain end to end"
```

Those numbers are not maintained by hand. Regenerate them any time, from the repository root:

```powershell
(Get-ChildItem packet-caps -Directory -Filter 'SC_*').Count   # how many recordings exist
.\sc-capture\out\sccap.exe coverage                           # how much is still unseen
```

Which scenarios you have already recorded, straight from the bundle names:

```powershell
Get-ChildItem packet-caps -Directory -Filter 'SC_*' |
    ForEach-Object { $_.Name.Split('__')[1] } | Sort-Object -Unique
```

And the mirror image — **everything still missing**, read out of the trackers in this
very file, so it can never drift out of date:

```powershell
$planned = Select-String -Path docs\CAPTURE-PHASES.md -Pattern '^- \[[ x]\] `SC-([A-Z0-9-]+)`' |
    ForEach-Object { $_.Matches[0].Groups[1].Value } | Sort-Object -Unique
$done = Get-ChildItem packet-caps -Directory -Filter 'SC_*' |
    ForEach-Object { $_.Name.Split('__')[1] } | Sort-Object -Unique
$planned | Where-Object { $_ -notin $done }
```

Right now that prints all 53 scenario IDs, because you have recorded none of them.

### The phase board

| Phase | In one line | Recordings | Status |
|---|---|---|---|
| [0](#phase-0--can-this-machine-record) | Can this machine record? | — | ✅ **Done** — two human items to confirm |
| [1](#phase-1--prove-the-chain-end-to-end) | Prove the whole chain works | 3 | ⬜ **← you are here** |
| [2](#phase-2--the-one-shots) | The one-shots you get once, ever | 4 | ⬜ |
| [3](#phase-3--menus-and-money) | Menus and money — the breadth phase | 12 | ⬜ |
| [4](#phase-4--into-a-match) | Into a match, and the UDP frontier | 17 | ⬜ |
| [5](#phase-5--open-space) | Open Space | 10 | ⬜ |
| [6](#phase-6--break-it-on-purpose) | Break it on purpose | 6 | ⬜ |
| [7](#phase-7--hard-mode) | Two clients, TLS keys, deep client archive | 1 + 2 tasks | ⬜ |
| [8](#phase-8--the-phase-with-no-deadline) | Decoding — no deadline, never finishes | — | ⬜ open forever |

53 recordings total: the 52 scenarios in the README checklist, plus the Tier-3
two-client capture in Phase 7.

**Update the Status column as you finish a phase.** It is the only hand-maintained
thing in this file, and it exists so a human can see the shape at a glance. Everything
else is derived from the recordings themselves.

---

## How a phase works

Every phase has the same four parts:

- **Goal** — what the archive gains, in plain terms.
- **Why it's here** — why this phase comes before the next one. Some of these orderings
  are irreversible mistakes if you get them wrong; those are flagged ⚠️.
- **Do this** — the commands, in order.
- **Done when** — a finish line you can verify. Where possible it is a command that
  exits 0 or a number that moved.

Then a **tracker** — tick the boxes as you go. The `comm` command above reads these.

One rule holds across every phase: **one scenario, one recording.** Never record login →
match → shopping as a single blob. The bundle name carries the scenario ID, so a blob is
mislabelled by construction and painful to interpret later.

---

## The loop you repeat in every phase

This is one complete recording, start to finish, exactly as you will do it 53 times. Read
it once here; the phases below just say *which* scenario and *what to do in-game*.

**Terminal 1 — check the wire, then record.** Both terminals must be **PowerShell running as
Administrator**; Npcap refuses capture handles to anything less. The game should already be
running and sitting in the hangar.

```powershell
cd $HOME\dev\star-conflict-clone

# 1. Which network connection actually carries game traffic? 30 seconds of sampling.
.\sc-capture\out\sccap.exe doctor --watch 30s
```

```
 * Ethernet     packets=1841    game=212    services=shard(180),chat(32)
   Wi-Fi        packets=12      game=0
```

The starred line is the one to record. If **nothing** is starred, stop — recording anyway
produces a perfectly valid file with none of the game in it. That is the only failure here
that is both silent and total.

```powershell
# 2. Start recording. Scenario ID without the SC- prefix (SC-AUTH-02 also works).
.\sc-capture\out\sccap.exe capture --scenario AUTH-02 --region EU --out packet-caps
```

A status line updates once a second:

```
[00:04:12] services=lb,shard,chat  frames=182401  journal=241MiB  drops=0  records=9188  novel=2
```

- **`drops` must stay at `0`.** Anything else means traffic crossed the wire and was not
  recorded. Stop, close whatever is loading the machine, start again.
- **`novel` counting up is good** — it means something appeared that the tool has never
  seen in any recording anywhere.
- `services=` naming lb/shard/chat is your confirmation you are on the right wire.

**Terminal 2 — the envelope.** This structure is what makes recordings comparable to each
other. Do it the same way every time.

```powershell
# 3. Wait 10 seconds doing absolutely nothing in-game first. This is the "background hum"
#    your action gets compared against. It is not optional padding; it is the control.

# 4. Mark the start.
.\sc-capture\out\sccap.exe mark "BEGIN AUTH-02"

# 5. Do the scenario, marking before and after each distinct action. Console mode keeps
#    a prompt open: type a label, press Enter, repeat. Ctrl+Z then Enter to finish.
.\sc-capture\out\sccap.exe mark --console
```

```
marking SC_20260814T203015Z__AUTH-02__vol-local__EU__000
> entering password
> pressed login
> hangar visible
```

Mark generously. A label costs nothing and turns *"some packets happened around here"*
into *"this is where the login button was pressed"*. `mark` finds the running capture by
itself — there is no session ID to pass.

```powershell
# 6. Mark the end, wait 10 more idle seconds, then Ctrl+C in Terminal 1.
.\sc-capture\out\sccap.exe mark "END AUTH-02"
```

> **Ctrl+C, not the window's X button.** Closing the console gives the process about two
> seconds before Windows kills it. The recording survives that and verifies as
> `interrupted`, but you lose the clean close and the final `SHA256SUMS`.

Want to check on a capture without disturbing it (figures refresh on the anchor interval,
so they lag by a few seconds):

```powershell
.\sc-capture\out\sccap.exe status
```

**After Ctrl+C — verify, score, write notes.**

```powershell
# 7. Is the recording sound?
.\sc-capture\out\sccap.exe verify packet-caps\SC_20260814T203015Z__AUTH-02__vol-local__EU__000
```

```
[  ok  ] schema       version 1.0
[  ok  ] termination  closed cleanly
[  ok  ] integrity    5 files hashed and matching
[  ok  ] segments     capture_00001_20260814203015.pcapng: 453 frames
[  ok  ] drops        no packets dropped
[  ok  ] clock        2 anchors, monotonic
[  ok  ] permissions  owner-only ACL (owner + SYSTEM)
[  ok  ] index        494 records, all frame references resolve

VERIFIED — 453 frames across 1 segment(s), mode=passive.
```

`VERIFIED (interrupted)` also exits 0 and is fine — a recording that was killed is valid
up to the point it stopped and is absolutely worth keeping. Only `FAILED` is a problem.

```powershell
# 8. What did this session contribute? The number that matters is "never observed".
.\sc-capture\out\sccap.exe coverage
```

```
Element coverage (404 known)
  message_type       39 known     4 decoded     3 undecoded    32 never observed
  async_request     249 known     2 decoded    19 undecoded   228 never observed
  notification      116 known     1 decoded     9 undecoded   106 never observed

Never observed: 366 — these die with the servers if nobody captures them.
```

```powershell
# 9. Two minutes of notes now saves someone an hour in 2031.
notepad packet-caps\SC_20260814T203015Z__AUTH-02__vol-local__EU__000\notes.md
```

That is the loop. Everything below is which scenario, in which order, and why.

> **Several `capture_*.pcapng` files in one bundle is normal.** The journal rotates every
> 200 MB or 10 minutes. It is one continuous recording split across files, not a fault.

---

## Phase 0 — Can this machine record?

**Status: ✅ done on this machine.** Two items below need a human to confirm.

### Goal

The machine can see the network, the clock is trustworthy, the tool knows exactly which
build of the game you are running, and the client itself is archived.

### Why it's here

Everything downstream is worthless if the recording is not really a recording. And the
client archive belongs here, not later, because **the game can update at any time** —
patch day silently destroys your ability to archive the build your recordings were made
against.

### Do this

Install [Npcap](https://npcap.com) first, then, in **PowerShell as Administrator**:

```powershell
cd $HOME\dev\star-conflict-clone\sc-capture
go build -tags npcap -o out\sccap.exe .\cmd\sccap
.\out\sccap.exe doctor
```

Verified on this machine today:

```
Host diagnosis

  [OK  ] capture backend        Npcap backend compiled in
  [OK  ] privileges             running elevated
  [OK  ] interfaces             one live interface: Ethernet
  [WARN] offloads (Ethernet)    cannot be determined here — if LSO or RSC is on, captured
                                frame boundaries and timings will be the driver's reconstruction
           Get-NetAdapterLso -Name "Ethernet"; Get-NetAdapterRsc -Name "Ethernet"
           Disable-NetAdapterLso -Name "Ethernet"; Disable-NetAdapterRsc -Name "Ethernet"
  [OK  ] clock                  the Windows Time service is running
  [OK  ] coverage store         C:\Users\ahopkins\AppData\Local\sccap is writable
  [OK  ] game client            Star Conflict, build 24666578, binary 3F2A9C10BE44 (codeview), I386 (32-bit)
  [OK  ] protocol tables        embedded element universe revision sc-proxy@968f1a3f

This machine can capture.
```

**The offload line always appears, and that is not a failure.** `doctor` deliberately does
not guess at LSO and RSC — the properties behind them have no stable names across vendors,
and a diagnostic that sometimes lies about frame fidelity is worse than one that admits it
cannot see. Run the two `Get-` commands it prints; if either says `Enabled`, run the
matching `Disable-`. Confirmed independently on this machine: both are off on `Ethernet`.

`doctor` never changes your system. It prints the exact command for anything it finds and
leaves the decision to you.

### Done when

```powershell
.\sc-capture\out\sccap.exe doctor; "exit=$LASTEXITCODE"   # exit=0, no FAIL, a "game client" line
```

- [x] Npcap installed
- [x] `sccap.exe` built with `-tags npcap` and `doctor` exits 0 with no `FAIL`
- [x] Terminal running as Administrator
- [x] LSO and RSC confirmed off
- [x] `doctor` identifies the game client automatically
- [ ] **Throwaway game account created** — ⚠️ recordings contain your login credentials in
      the clear. Never a primary account.
- [ ] **Game client archived** — the only way to recover a message's structure if you never
      recorded that message. See README §2.6 for the commands; the executables and DLLs,
      not the multi-gigabyte assets.

---

## Phase 1 — Prove the chain end to end

**3 recordings. About 30 minutes. Do not skip to Phase 3 because this looks like busywork.**

### Goal

Turn "the tool builds" into "I have made a real recording, verified it, and watched the
never-observed count fall". Three cheap recordings that are also permanently useful.

### Why it's here

You want to discover a wrong interface, a full disk, or a misunderstanding of the envelope
**now**, on a recording that costs nothing to redo — not during the one-shot new-account
login in Phase 2, which you cannot redo at any price.

The two baselines here are also the reference material every later recording is read
against. Traffic that appears in the hangar doing nothing is *not* interesting; the whole
point of a baseline is to let someone subtract it.

> ⚠️ **Trap — do not use a never-logged-in account for this phase.** An account's first
> ever login happens exactly once and is scenario `SC-AUTH-01` in Phase 2. If your only
> throwaway account is brand new, **make a second one now** and use that here. Burning
> `AUTH-01` on a smoke test is the most expensive mistake in this document.

### Do this

Run [the loop](#the-loop-you-repeat-in-every-phase) three times.

**1. `SC-BASE-03` — client launched, never logged in.** Start recording *before* launching
the game. Sit at the login screen for 60 seconds. Do not log in. Stop.

```powershell
.\sc-capture\out\sccap.exe capture --scenario BASE-03 --region EU --out packet-caps
```

**2. `SC-AUTH-02` — cold login, established account.** Game fully closed. Start recording,
10 idle seconds, launch, log in, wait until the hangar is fully loaded and idle, mark
`END`, 10 idle seconds, stop.

**3. `SC-BASE-01` — hangar idle baseline.** Logged in, sitting in the hangar. Record 3–5
minutes of *touching absolutely nothing*. Do not move the mouse over ship cards. Nothing.

### Done when

```powershell
# all three verify
Get-ChildItem packet-caps -Directory -Filter 'SC_*' | ForEach-Object {
    .\sc-capture\out\sccap.exe verify $_.FullName *> $null
    $r = if ($LASTEXITCODE -eq 0) { '  ok   ' } else { 'FAILED ' }
    "$r $($_.Name)"
}

# and the score moved
.\sc-capture\out\sccap.exe coverage
```

- Three bundles exist and all three verify
- `drops` was `0` in all three
- **Never observed is below 404.** If it is still 404 after a successful login recording,
  something is wrong — you almost certainly recorded the wrong interface. Go back to
  `doctor --watch`.

### Tracker

- [ ] `SC-BASE-03` — Client launched, never logged in
- [ ] `SC-AUTH-02` — Cold login, established account `P0`
- [ ] `SC-BASE-01` — Hangar idle baseline `P0`

---

## Phase 2 — The one-shots

**4 recordings. ⚠️ Contains the only genuinely irreplaceable recording in the project.**

### Goal

Capture the events that exist exactly once per account, and the authentication failure
paths that no one ever thinks to record.

### Why it's here

Immediately after Phase 1 and before anything else, because it needs a **brand-new account
that has never logged in**, and every day you leave it is a day you might use that account
by accident. Once it has logged in once, `SC-AUTH-01` is gone forever — not "harder", gone.

### Do this

**`SC-AUTH-01` — brand-new account, first ever login and tutorial. ⚠️ IRREPLACEABLE.**
Create the account, then record from before the very first login through the entire
tutorial and first rewards. This one is allowed to run long; ignore the 30-minute
guideline. Mark heavily — every tutorial step.

```powershell
.\sc-capture\out\sccap.exe capture --scenario AUTH-01 --region EU --out packet-caps
```

**`SC-AUTH-04` — failed authentication, every variant.** Cheap, fast, routinely missing.
Wrong password. Non-existent username. Empty password. Correct password with the wrong
capitalisation. Mark each attempt so they can be told apart:

```powershell
.\sc-capture\out\sccap.exe mark --console
> attempt 1: wrong password, correct user
> attempt 2: user does not exist
> attempt 3: empty password
```

**`SC-AUTH-03` — warm login.** Close the client and reopen it while the session is still
cached, so it logs in without asking. The difference between this and `AUTH-02` is exactly
the credential exchange.

**`SC-AUTH-05` — clean logout and client exit.** Log out through the menu, then quit.
Keep recording for 10 seconds after the process is gone.

### Done when

- All four verify, `drops=0`
- Every one of the four `AUTH-*` scenarios appears in `ls packet-caps/`
- `notes.md` in the `AUTH-01` bundle records what the account was, when it was created,
  and what the tutorial made you do

### Tracker

- [ ] `SC-AUTH-01` — Brand-new account: first ever login and tutorial `P0` ⚠️ irreplaceable
- [ ] `SC-AUTH-04` — Failed authentication, every variant `P0`
- [ ] `SC-AUTH-03` — Warm login (cached session / auto-login)
- [ ] `SC-AUTH-05` — Clean logout and client exit

---

## Phase 3 — Menus and money

**12 recordings. The highest coverage-per-minute in the whole project.**

### Goal

Sweep the hangar, inventory, loadout, store and contracts. This is where the bulk of the
249 `AC_*` request codes and 116 `SN_*` notifications actually live.

### Why it's here

It is safe, it is repeatable, it needs no other players, and it moves the never-observed
number more per hour than anything else. If you only ever have quiet weekday evenings,
spend them here.

### Do this

One scenario per recording, as always. The pattern for all twelve is identical: idle 10s →
`BEGIN` → do the single thing → `END` → idle 10s.

> **Write your exact balances into `notes.md`, before and after — credits, GS,
> monocrystals.** This is the single highest-leverage two minutes in the project. Those
> numbers let someone search the raw bytes for `4 812 350`, land directly on the currency
> field, and skip hours of guesswork. Do it for every `ECON` and `HGR` recording.

```powershell
Add-Content packet-caps\SC_..._ECON-01_...\notes.md @'
Balances before: credits 4812350, GS 1240, monocrystals 88
Bought: Mk3 shield booster, 12500 credits
Balances after:  credits 4799850, GS 1240, monocrystals 88
'@
```

**Record the failures too.** `ECON-03` (buy something you cannot afford) and `ECON-04`
(other rejections — wrong ship, level-locked, already owned) are as valuable as the
successes and take ninety seconds each. A reimplementation that only knows the happy path
is a reimplementation that falls over the first time a player is broke.

### Done when

- All twelve verify, `drops=0`
- Every `ECON-*` and `HGR-*` bundle has balances in `notes.md`
- Never-observed has dropped substantially — this is the phase where you should watch
  `async_request` fall hardest:

```powershell
(.\sc-capture\out\sccap.exe coverage --kind async_request --state never_observed).Count
```

### Tracker

- [ ] `SC-HGR-01` — Ship swap
- [ ] `SC-HGR-02` — Module fit / unfit
- [ ] `SC-HGR-03` — Inventory operations
- [ ] `SC-HGR-04` — Ship upgrade / synergy / progression spend
- [ ] `SC-HGR-05` — Ellydium / crafting-tree progression *(if in your build)*
- [ ] `SC-ECON-01` — Purchase with soft currency `P0`
- [ ] `SC-ECON-02` — Sell / refund
- [ ] `SC-ECON-03` — Purchase failure: insufficient funds `P0`
- [ ] `SC-ECON-04` — Other rejection paths `P0`
- [ ] `SC-ECON-05` — Premium currency spend
- [ ] `SC-ECON-06` — Player market / trading *(if in your build)*
- [ ] `SC-ECON-07` — Contracts, daily missions, rewards

---

## Phase 4 — Into a match

**17 recordings. Contains the single most valuable message in the game.**

### Goal

Record the handoff from the lobby to a match server, and then the actual gameplay traffic
on the other side of it.

### Why it's here

`SC-MM-04` — the lobby → match server handoff — hands the client the address of a
dedicated game server. **Everything after that message is traffic no tool in this project
has ever recorded.** The master-server half is partly understood; the UDP gameplay half is
a blank page. Recording at the wire archives it whether or not anything currently
understands a byte of it, which is the entire reason this project records rather than
decodes.

Phases 1–3 come first because this phase is where a mistake costs the most time: matches
involve other people, cannot be paused, and cannot be repeated on demand.

### Do this

**Non-competitive modes only.** Never degrade a real match for uninvolved players.

**Start with `SC-BASE-02` — in-match idle baseline.** Enter a match and do nothing at all.
Then record the combat scenarios **on the same map, in the same play session** where you
can. The comparison only works if the two recordings differ in one thing.

Then the matchmaking chain, one recording each:

```powershell
.\sc-capture\out\sccap.exe capture --scenario MM-04 --region EU --out packet-caps
```

Then the combat isolation series. These are "do exactly one thing, and only one thing"
recordings, and their value comes entirely from that discipline:

- `CBT-01` — rotate on **one** axis. Yaw only. Not pitch, not roll, not thrust.
- `CBT-02` — translate on one axis. No rotation.
- `CBT-05` then `CBT-06` — fire and deliberately **miss**, then fire and **hit**. The
  difference between those two otherwise-identical recordings is what identifies the
  hit-registration field.

Mark every single repetition:

```powershell
.\sc-capture\out\sccap.exe mark --console
> yaw left x3
> centred, 5s still
> yaw right x3
```

### Done when

- `SC-MM-04` recorded and verified — after which:

```powershell
# did the handoff message actually land in the recording?
.\sc-capture\out\sccap.exe decode packet-caps\SC_*MM-04*/ --type SCMD_CONNECT_DEDICATED_SERVER
```

- `SC-BASE-02` recorded on the same map as at least one `CBT-*` recording
- All 17 verify with `drops=0` — drops matter most here, because in-match UDP is the
  highest packet rate you will ever record. If drops appear, close everything else and
  redo it.

### Tracker

- [ ] `SC-BASE-02` — In-match idle baseline `P0`
- [ ] `SC-MM-01` — Solo queue join and cancel
- [ ] `SC-MM-02` — Squad formation
- [ ] `SC-MM-03` — Squad queue and synchronised match entry
- [ ] `SC-MM-04` — Lobby → match server handoff `P0`
- [ ] `SC-MM-05` — Match exit and return to lobby
- [ ] `SC-MM-06` — Custom / private match *(if available)*
- [ ] `SC-CBT-01` — Single-axis rotation isolation `P0`
- [ ] `SC-CBT-02` — Translation isolation
- [ ] `SC-CBT-03` — Afterburner / overdrive state
- [ ] `SC-CBT-04` — Module and ability activation
- [ ] `SC-CBT-05` — Weapon fire: miss `P0`
- [ ] `SC-CBT-06` — Weapon fire: hit registration `P0`
- [ ] `SC-CBT-07` — Damage taken: shield and hull `P0`
- [ ] `SC-CBT-08` — Missile lock and launch
- [ ] `SC-CBT-09` — Death and respawn
- [ ] `SC-CBT-10` — Objective interaction

---

## Phase 5 — Open Space

**10 recordings.**

### Goal

The persistent world: sector transitions, docking, NPCs, loot, quests.

### Why it's here

Open Space is a different traffic profile from both the hangar and instanced combat — a
persistent world with server-driven spawns and state that survives your logout. It comes
after Phase 4 because it reuses the same in-match recording discipline, and because you
want the combat isolation recordings in hand first to read the NPC fights against.

These sessions are allowed to run long; `SC-WLD-*` is exempt from the 30-minute guideline.

### Do this

The high-value four, if you are short of time: `WLD-01` (entering Open Space), `WLD-02`
(sector transition through a warp gate), `WLD-05` (NPC spawn triggers) and `WLD-07` (loot
and cargo).

Mark the moment of each transition precisely — a warp gate is a hard boundary in the
traffic and a marker on it is worth a great deal:

```powershell
.\sc-capture\out\sccap.exe mark "entering warp gate to sector 2"
.\sc-capture\out\sccap.exe mark "loading screen ends, sector 2 visible"
```

### Done when

- All ten verify, `drops=0`
- Sector names and coordinates noted in `notes.md` — where you went, in what order

### Tracker

- [ ] `SC-WLD-01` — Enter Open Space `P0`
- [ ] `SC-WLD-02` — Sector transition / warp gate `P0`
- [ ] `SC-WLD-03` — Station docking and undocking
- [ ] `SC-WLD-04` — Navigation and persistence
- [ ] `SC-WLD-05` — NPC / alien spawn triggers `P0`
- [ ] `SC-WLD-06` — NPC combat and AI behaviour
- [ ] `SC-WLD-07` — Loot and cargo `P0`
- [ ] `SC-WLD-08` — Death in Open Space
- [ ] `SC-WLD-09` — Quests / missions
- [ ] `SC-WLD-10` — Special Operations / raids *(if available)*

---

## Phase 6 — Break it on purpose

**6 recordings. Thirty seconds each. Routinely missing from every archive.**

### Goal

What the protocol does when things go wrong: dropped connections, timeouts, kicks, version
mismatches.

### Why it's here

Every reimplementation gets error handling wrong, because nobody thinks to record it. It
sits after the happy paths only because you need to know what normal looks like first —
otherwise you cannot tell which part of the recording is the failure.

Pulling your own network cable mid-match takes thirty seconds and is genuinely one of the
best value recordings in this document.

### Do this

**Your own uplink only.** Never anything aimed at the servers, and nothing that affects
another player's match.

```powershell
.\sc-capture\out\sccap.exe capture --scenario EDGE-01 --region EU --out packet-caps
```

For `EDGE-01`, mid-match, with the recording running:

```powershell
.\sc-capture\out\sccap.exe mark "pulling network in 3s"
# Unplug the ROUTER's uplink, not your own cable -- see below.
# ...wait 90 seconds...
.\sc-capture\out\sccap.exe mark "restoring network"
```

> **Break the path upstream, not at your own machine.** If you disable your adapter or pull
> your own cable, the client's retry packets are never transmitted and so are never
> recorded — and the retries are the whole point. Unplug the router's uplink, or pull the
> cable at the switch: your adapter stays up, the client keeps retransmitting into a void,
> and every attempt lands in the file. Keep the capture running throughout; do not stop and
> restart. Manual §3.7 has two further methods and says which to record in `notes.md`.

`EDGE-03` (idle timeout) is the one that needs patience rather than effort: log in, walk
away, leave the recording running until the server gives up on you.

### Done when

- All six verify — several will legitimately show `VERIFIED (interrupted)` or long quiet
  stretches; both are fine
- `notes.md` records exactly what you did and when, in wall-clock terms

### Tracker

- [ ] `SC-EDGE-01` — Network loss mid-match `P0`
- [ ] `SC-EDGE-02` — Client hard kill and reconnect
- [ ] `SC-EDGE-03` — Idle timeout
- [ ] `SC-EDGE-04` — Server-initiated disconnect
- [ ] `SC-EDGE-05` — Degraded network conditions *(your own uplink only)*
- [ ] `SC-EDGE-06` — Client version mismatch

---

## Phase 7 — Hard mode

**1 recording plus 2 archival tasks. Needs a second machine, or patience.**

### Goal

Three things that need more than one person, more than one machine, or more setup than a
normal session: a synchronised two-client capture, TLS key material, and a deep archive of
the client.

### Why it's here

Last among the deadline-bound phases because it is the most logistically expensive, not
the least valuable. `SC-T3-01` in particular answers a question no single-client recording
can: **which parts of a message are the same for both players and which differ.** That is
the difference between guessing at a field and knowing it.

### Do this

**`SC-T3-01` — two clients recording the same match simultaneously.** Two machines, two
accounts, same match, both recording, clocks synchronised. Note the other machine's bundle
ID in your `notes.md` and vice versa — that cross-reference is what makes the pair usable.

Bundles are byte-identical in structure whichever machine records them, so the second
machine need only be running `sccap`. To fold the second machine's coverage into this
machine's scoreboard, copy the bundle over and:

```powershell
.\sc-capture\out\sccap.exe coverage --ingest packet-caps\SC_20260814T210000Z__T3-01__vol-b__EU__000
```

```
Folded 214 observation(s) from coverage-delta.json.
```

**TLS key material.** The game's web traffic is encrypted; the main game protocol is not.
Keeping the session keys makes the encrypted half readable later. This is a manual step —
`sccap` does not manage keys. Set `SSLKEYLOGFILE` in the Steam launch options, confirm the
file is actually growing, and keep it **in the bundle directory** next to the pcapng so the
two never get separated. Full procedure: capture manual §1.9.

**Deep client archive.** Phase 0 archived the executable. This is the thorough version —
the engine archives (`.vromfs.bin`), configs (`.blk`), the client's local state, and all three
version identifiers. Capture manual §6.1.

### Done when

- Two bundles from two machines cover the same match, cross-referenced in each `notes.md`
- A key log file sits inside at least one bundle that contains TLS traffic
- The deep client archive exists somewhere off this machine

### Tracker

- [ ] `SC-T3-01` — Two clients recording the same match simultaneously
- [ ] TLS key material captured for the auth flow — manual §1.9
- [ ] Deep client archive: engine archives, configs, local state, version identifiers — manual §6.1

---

## Phase 8 — The phase with no deadline

**Never finishes. Do not let it compete with Phases 1–7 while the servers are up.**

### Goal

Turn recordings into understanding.

### Why it's here — and why it's last

An element that was recorded but not understood is safe forever; someone decodes it in
2031 and it works, because the bytes are still there. An element that was never recorded is
gone. **Seen but not understood is completely fine. Never seen is fatal and permanent.**

So: while the servers are up, breadth of capture beats depth of decoding, every time. This
phase is what you do on the days you cannot play, and every day after the shutdown.

### Do this

```powershell
# What is in a recording, in decoded form?
.\sc-capture\out\sccap.exe decode packet-caps\SC_*AUTH-02*/ --limit 40

# What surprised the tool? These are the interesting ones.
.\sc-capture\out\sccap.exe decode packet-caps\SC_*AUTH-02*/ --status unknown_element

# What is still not understood, by name?
.\sc-capture\out\sccap.exe coverage --state observed_undecoded

# Improved the decoder? Re-read every old recording with it. The evidence never changes.
.\sc-capture\out\sccap.exe index packet-caps\SC_*AUTH-02*/ --rebuild
.\sc-capture\out\sccap.exe verify packet-caps\SC_*AUTH-02*/
```

`--rebuild` regenerates the derived index from the raw pcapng alone, then re-checks that
the raw journal's hashes are unchanged. Records that read `undecoded` today decode as
decoders improve. All of this works with every game server unreachable — which is the
entire point.

Post-shutdown, the manual's §6.2 covers pointing the client at a local sink and recording
what it *tries* to say. That is a real capture avenue that outlives the servers.

### Done when

Never. That is the design.

---

## Keeping score

Three independent records of what has been done. They agree, and if they ever disagree the
recordings win.

**1. The machine-wide coverage store** — `%LOCALAPPDATA%\sccap\coverage.json`, updated
automatically at the end of every capture. Survives restarts, spans every recording you
have ever made.

```powershell
.\sc-capture\out\sccap.exe coverage                                   # the summary
.\sc-capture\out\sccap.exe coverage --state never_observed            # everything still missing
.\sc-capture\out\sccap.exe coverage --kind notification --state never_observed
.\sc-capture\out\sccap.exe coverage --json                            # for scripting
```

State moves in one direction only: `never_observed` → `observed_undecoded` → `decoded`. It
never regresses, so the number is a genuine ratchet.

**2. Per-bundle deltas** — every bundle contains `coverage-delta.json` recording what *that
session* contributed. This is why the store is rebuildable: if you lose it, or move to a
new machine, fold the bundles back in one by one:

```powershell
Get-ChildItem packet-caps -Directory -Filter 'SC_*' |
    ForEach-Object { .\sc-capture\out\sccap.exe coverage --ingest $_.FullName }
```

**3. This document** — the phase board and the trackers, for the human question of "what am
I supposed to do next Tuesday".

### A one-shot progress report

Paste this into a PowerShell window from the repository root. It is deliberately not a
script file: **no scripts ship with this project**, in any language (constitution,
Additional Constraints). Anything a contributor is required to run is a `sccap` subcommand;
this is a convenience you type, not a deliverable.

```powershell
"Bundles:"
$done = @()
Get-ChildItem packet-caps -Directory -Filter 'SC_*' -ErrorAction SilentlyContinue | ForEach-Object {
    .\sc-capture\out\sccap.exe verify $_.FullName *> $null
    $r = if ($LASTEXITCODE -eq 0) { 'ok    ' } else { 'FAILED' }
    "  $r  $($_.Name)"
    $done += $_.Name.Split('__')[1]
}
if (-not $done) { "  (none yet)" }

""
"Scenarios still missing:"
Select-String -Path docs\CAPTURE-PHASES.md -Pattern '^- \[[ x]\] `SC-([A-Z0-9-]+)`' |
    ForEach-Object { $_.Matches[0].Groups[1].Value } |
    Sort-Object -Unique | Where-Object { $_ -notin $done } | ForEach-Object { "  $_" }

""
.\sc-capture\out\sccap.exe coverage
```

### Session ledger

Optional, but useful for spotting patterns — a run of `drops>0` means something changed on
your machine. Add a row after every recording.

| Date | Bundle ID | Scenario | Drops | Verify | Never observed after | Notes |
|---|---|---|---|---|---|---|
| | | | | | 404 (start) | nothing recorded yet |

---

## When something goes wrong

| What you see | What it means | What to do |
|---|---|---|
| `drops` above 0 | The kernel threw packets away before they reached the file. The recording has holes. | Stop. Close browsers, updaters, sync clients. Record again. Not fixable afterwards. |
| `doctor --watch` stars nothing | No interface is carrying game traffic — or the game is not actually connected. | Confirm the game is logged in and in the hangar. Check VPNs. Never record "just in case" on a guess. |
| `services=` never shows `shard` | You are recording an interface the game does not use. | Rerun `doctor --watch 30s` and pass `--interface` explicitly. |
| Coverage did not move after a session | Almost always the wrong interface. Occasionally a scenario that genuinely repeats known traffic. | Check the recording has non-trivial payload: `sccap decode <bundle> --limit 20`. |
| `verify` says `FAILED` | A hash mismatch or structural inconsistency. Exit code 2. | Read the `FAIL` lines. Do not delete the bundle — a failed verify is still evidence. |
| `VERIFIED (interrupted)` | The capture was killed rather than stopped cleanly. Exit code 0. | Nothing. It is valid up to where it stopped and worth keeping. |
| Capture stopped by itself | Free space hit the hard floor. Exit code 4, session closed cleanly. | Free up disk. Raise `--floor` only if you know what you are doing. |
| `sccap mark` says no session | The capture is not running, or was killed. | Check with `sccap status`. Marks can be added to a finished bundle with `--bundle <dir>`. |
| Something weird happened in-game | Possibly the most valuable thing you will record all week. | **Do not delete it.** `sccap mark "ANOMALY <what happened>"`, finish the envelope normally, write it up in `notes.md`. |

**Exit codes**, if you script any of this: `0` ok · `1` bad usage · `2` verify failed ·
`3` cannot capture on this host · `4` stopped at the disk floor · `5` bundle written by a
newer schema.

> **The capture manual is current.** It was rewritten for Windows alongside this document
> (constitution v3.0.0), and its §4 and §5 now describe `sccap verify` rather than the Python
> helpers an earlier version of this project shipped. If you find a reference to `tools/`,
> `verify_capture.py` or a Linux-only step anywhere outside a passage explicitly describing
> history, that is a bug in the document — please report it.

---

## Two builds, and which one you have

Everything above assumes the capture build. There is a second one worth knowing about.

| | Capture build | Offline build |
|---|---|---|
| Command | `go build -tags npcap -o out\sccap.exe .\cmd\sccap` | `go build -o out\sccap.exe .\cmd\sccap` |
| Needs | Npcap, a C compiler, an elevated terminal | Nothing at all |
| `capture`, `doctor --watch` | ✅ | ❌ |
| `verify`, `decode`, `index`, `coverage`, `status` | ✅ | ✅ |

`doctor` tells you which one you are holding. The offline build is enough to work through
**Phase 8 entirely** — reading, re-decoding and scoring archived recordings — on a machine
with nothing installed, which is the point: analysing somebody else's captures should not
require the driver that made them.

Npcap cannot be bundled with this project (its licence forbids redistribution) and needs
cgo, which is why the split exists at all. The cost is charged only to contributors who
actually record.

---

## The rules, in one place

- Record **only your own traffic**, passively. No fuzzing, no injection, no traffic aimed
  at live servers beyond normal play.
- Combat and feasibility work uses **non-competitive modes only**. Never degrade a real
  match for uninvolved players.
- Recordings contain session tokens and credentials in the clear. **Throwaway accounts,
  always.** Bundles are created readable only by you, and nothing is ever sent anywhere —
  the binary has no upload path at all. Sharing is a manual decision you make.
- `packet-caps/` and `captures/` are git-ignored. Keep it that way.
- No game assets or client binaries are redistributed here. Archive your own, and keep it.

---

## Plain-English glossary

**Bundle** — one recording, as a folder. Named
`SC_<UTC timestamp>__<SCENARIO>__<volunteer>__<REGION>__<seq>`, e.g.
`SC_20260814T203015Z__AUTH-02__vol-local__EU__000`. Contains the raw `.pcapng` segments,
`session.json`, the derived index, your markers, checksums and `notes.md`. Only the
`.pcapng` files and `session.json` are irreplaceable; everything else can be regenerated.

**Element** — one of the 404 things the protocol is known to be able to say: 39 message
types, 249 `AC_*` request codes, 116 `SN_*` notifications. Pulled from the game's own
program file, which is how we know their names before ever seeing one.

**Never observed** — an element that has not appeared in any recording you have made. The
countdown. It is the only number in this project with a deadline attached.

**Drops** — packets the operating system threw away because it could not keep up. Holes in
the recording. Must be zero; cannot be repaired afterwards.

**Offloads** — a network card feature that glues packets together before your computer sees
them. Good for speed, bad for recording: what lands in the file is your driver's
reconstruction rather than what actually crossed the wire. Turn them off.

**Marker** — a labelled moment you stamp onto the recording's timeline with `sccap mark`.
Free, and it turns anonymous traffic into an annotated one.

**Envelope** — the fixed structure every recording uses: 10 idle seconds, `BEGIN`, the
scenario with marks throughout, `END`, 10 idle seconds. It is what makes two recordings
comparable.

**Baseline** — a recording of doing nothing, so someone can subtract "the game idling" from
"the game doing the thing".

**Passive capture** — copying packets off the wire without taking part. The game talks
directly to the real servers exactly as normal; we are a tape recorder clipped to the
cable, not an operator in the middle of the call.
