# Star Conflict Network Archival Protocol

**Version 3.0 — Windows Volunteer Execution Manual**

This is an operational manual, not a tutorial. Follow it literally. Every rule here exists
because violating it produces a capture that cannot be used for protocol reconstruction after
the servers are gone.

**Platform: Windows 10 or 11.** The game is a Windows title, and it and the recorder run on the
same machine. If a step appears to require another operating system, it is a bug in this document
— report it.

**Tooling: one binary, `sccap`.** No scripts ship with this project, in any language. Anything
this manual asks you to run is either a `sccap` subcommand or a stock Windows command. Where a
command needs Administrator rights it says so; assume PowerShell throughout.

**Canonical source:** this file. Submit corrections as PRs against this document.

---

## 0. Read This Before You Capture Anything

### 0.1 What makes a capture valuable

A PCAP is only useful to an emulator developer if the developer can answer three questions
about every byte in it:

1. **When** did this arrive, to millisecond precision, on a clock we can correlate?
2. **What was the player doing** at that instant?
3. **What did the game state look like** before and after?

A 4-hour unannotated capture of "some gameplay" answers none of these and is close to
worthless. A 90-second capture of a single, isolated, annotated action answers all three and is
worth more than the 4-hour file. **Isolation and annotation beat volume.** This principle drives
every rule in this document.

### 0.2 The three techniques that do most of the work

Everything in Section 3 is an application of these. Understand them first.

**Differential capture.** Perform the same action N times (N ≥ 3), changing exactly one
variable. Bytes that stay constant across repetitions are opcodes and type tags. Bytes that
change every time are sequence numbers, nonces, and timestamps. Bytes that change only when your
one variable changes are *the field you are looking for*. A single capture of an action
localizes nothing; five controlled repetitions localize the field.

**Known-plaintext seeding.** Deliberately inject distinctive ASCII into game state before
capturing, so it can be found in the byte stream and used as an anchor for string encoding,
length prefixes, and struct offsets. Details in §2.7. This is the single cheapest way to make a
capture more useful and almost nobody does it.

**Baseline subtraction.** Capture the system doing *nothing* before capturing it doing
something. The idle-in-hangar and idle-in-match baselines (SC-BASE-01/02) reveal keepalive
cadence, snapshot tick rate, and the "null" shape of the delta-compressed state stream. Without
them, every field in a combat capture looks like it might be meaningful.

### 0.3 Ground rules — read fully, these are not optional

- **Passive capture only.** Do not fuzz, replay, inject, or otherwise send malformed traffic to
  live servers. It risks your account (ending your ability to contribute), it is legally
  distinct from observing your own traffic, and it can corrupt other players' sessions. The only
  traffic-shaping permitted is **loss/latency induction on your own uplink** (SC-EDGE-05), which
  affects only you, and only in Practice/PvE.
- **Use throwaway accounts wherever the scenario allows.** Your capture bundle *will* contain
  your session token and may contain credential-adjacent material. Assume every bundle you
  submit is readable by everyone who ever downloads the archive.
- **Never capture while entering payment details.** Stop the capture, complete the purchase,
  restart the capture. Real-money flows are out of scope; in-game currency flows are in scope.
- **Do not redact protocol data.** Auth handshakes, tokens, and account IDs are exactly what the
  emulator needs. Protect the bundle instead of sanitizing it (§5.3).
- **Other players appear in your captures.** That is unavoidable and inherent to the medium. Do
  not go out of your way to capture identifying information beyond normal gameplay, and do not
  publish raw bundles to public indexes without the coordination described in §5.3.

### 0.4 Your rig — one machine, and why that is the right one

The game and the recorder run on the same Windows machine. There is no compatibility layer, no
second box, and no routing to arrange. If you can play Star Conflict, you can record it.

**Why this matters beyond convenience.** A capture taken through a compatibility layer on another
operating system would carry the game's payload bytes intact — the client writes bytes to a socket
and they reach the wire unchanged. What it would *not* carry is anything beneath the payload. TCP
options, segmentation behaviour, keepalive defaults, retransmission timing and MTU would all
belong to the host stack rather than to the game running as shipped. Those are precisely the
questions §3.7 exists to answer and that per-record timestamps exist to make answerable, so
recording where the game actually runs is what makes them mean anything.

**Advanced, entirely optional: a mirror-port tap.** If you have a managed switch with port
mirroring, or a second machine with two NICs bridging the gaming machine to the router, capturing
there gives you frames untouched by your gaming machine's NIC offloads and puts zero capture load
on the client. It is strictly better wire fidelity, and it is more setup than most contributors
need. `sccap` runs the same way on the capture host. Set `host.tap: true` in the sidecar so a
reader knows the frames were observed off-box.

Do not treat the tap as a prerequisite. **An ordinary same-machine capture with the offloads
turned off is a good capture**, and volume of those beats a perfect rig that never gets used.

### 0.5 Capture tiers

Contribute at whatever tier you can sustain. A large volume of correct Tier 1 is worth more than
a handful of botched Tier 3 attempts.

| Tier | Requirements | Adds |
|---|---|---|
| **T1** | Wired Ethernet, quiesced host, `sccap capture` + markers + a screen recording | The baseline everyone can do |
| **T2** | + offloads disabled, endpoint inventory recorded, TLS key material where obtainable | Trustworthy framing, exact endpoint inventory, decryptable auth |
| **T3** | + Two synchronized clients in one match, or a mirror-port tap, or loss/latency profiles | Authoritative-vs-local field discrimination, reliability-layer discovery |

Tier 2 is mostly a matter of five minutes in §1.2 rather than extra hardware. Tier 3 needs either
a second account and machine, or a switch that can mirror.

---

## 0.6 P0 TRIAGE — If You Only Get Ten Captures, Get These

Ordered by irreplaceability. Items 1 and 2 in particular **cannot be reconstructed after
shutdown at any price** and should be prioritized above all polished work.

| # | Capture | Scenario | Why irreplaceable |
|---|---|---|---|
| 1 | **Brand-new account, first ever login + tutorial** | SC-AUTH-01 | The full initial account state dump and first-run flow. Once you have played, you can never produce this again. Burn several throwaway accounts on this. |
| 2 | **Cold login → hangar, several distinct account states** | SC-AUTH-02 | The full-state sync blob is the entire account data model on the wire. Capture from a new, a mid-progression, and a maxed account. |
| 3 | **One complete PvP match, entry to score screen** | SC-MM-04 + SC-CBT-* | Lobby→match handoff, roster packet, full combat session lifecycle. |
| 4 | **Idle baselines (hangar and in-match)** | SC-BASE-01/02 | Cheap, fast, and makes every other capture interpretable. |
| 5 | **Economy: buy, sell, upgrade — plus the failure cases** | SC-ECON-* | Failure responses reveal the server's validation table, which the emulator must reimplement. |
| 6 | **Open Space: sector transition, dock, NPC fight, loot** | SC-WLD-* | World persistence and AI triggers, entirely absent from arena captures. |
| 7 | **TLS key material for the auth/launcher flow** | §1.9 | Hard here, and worth attempting anyway — read §1.9 before spending time on it. |
| 8 | **Disconnect / reconnect / timeout / kick** | SC-EDGE-* | The paths every emulator gets wrong, and that nobody thinks to capture. |
| 9 | **Two-client synchronized match capture** | SC-T3-01 | The only way to distinguish server-authoritative fields from client-local ones. |
| 10 | **Client binaries and version manifest** | §6.1 | PCAPs without the matching client build are frequently undecodable. |

---

## 1. Environment Configuration (Windows)

### 1.1 One-shot setup

Three installs and a build. Full step-by-step with screenshots-in-prose is in
[Part 2 of the README](../README.md#part-2--setup-once-per-machine); this is the operational
summary.

1. **[Npcap](https://npcap.com)** — the packet driver. Accept the defaults.
2. **[Go 1.26+](https://go.dev/dl/)** — to build the tool.
3. **[Wireshark](https://www.wireshark.org/)** *(optional but recommended)* — not for capturing,
   for reading captures afterwards and for the independent cross-check in §4.

```powershell
cd sc-capture
go build -tags npcap -o out\sccap.exe .\cmd\sccap
```

Then, in an **Administrator** PowerShell, before every session:

```powershell
.\out\sccap.exe doctor
```

It is fast, it names the exact remedy for anything it finds, and it never changes your machine
itself. Run it every time — it catches the regressions that silently ruin captures.

> **Administrator is not optional.** Npcap refuses capture handles to unprivileged processes.
> `doctor` reports this as a distinct `privileges` line rather than letting it surface later as a
> mysterious failure to open an interface.

### 1.2 Host preparation

**Physical layer**
- [ ] **Use wired Ethernet.** Wi-Fi introduces retransmission artifacts and driver-level
      reordering that corrupt timing analysis. If you must use Wi-Fi, record it honestly in the
      sidecar so devs discount your inter-arrival timings.
- [ ] Disable any VPN or "gaming accelerator". They re-encapsulate traffic and destroy the
      endpoint inventory. Check with `Get-NetAdapter` and `Get-NetRoute -DestinationPrefix 0.0.0.0/0`.
- [ ] Leave IPv6 enabled and capture it. Accidentally excluding an IPv6 channel is a silent,
      total data loss.
- [ ] Watch for Hyper-V, WSL and VirtualBox virtual adapters. They make `doctor` report several
      live interfaces, and picking one of them records nothing. §1.3 is how you tell.

**NIC offload — this one matters and is usually skipped**

Large Send Offload and Receive Segment Coalescing make the adapter hand up *coalesced*
super-frames that never existed on the wire. Every byte is still captured, but the framing and
timing become your driver's reconstruction, which destroys MSS boundaries and any inter-arrival
measurement.

```powershell
# Look first — these report per-adapter, per-direction:
Get-NetAdapterLso  -Name "Ethernet"
Get-NetAdapterRsc  -Name "Ethernet"

# Turn them off for the session (Administrator):
Disable-NetAdapterLso -Name "Ethernet"
Disable-NetAdapterRsc -Name "Ethernet"

# Afterwards, if you want them back:
Enable-NetAdapterLso -Name "Ethernet"
Enable-NetAdapterRsc -Name "Ethernet"
```

`sccap doctor` deliberately does **not** report whether these are on. The underlying properties
are per-driver with no stable names across vendors, so any answer it gave would sometimes be
wrong, and a diagnostic that lies about frame fidelity is worse than one that says "look for
yourself". It prints the commands above instead.

Some adapters do not implement LSO or RSC at all, in which case the `Get-` cmdlet errors rather
than reporting `False`. That is fine — there is nothing to disable. Record in the sidecar what you
actually turned off. If you could not turn something off, say so: a reader will treat your frame
boundaries as untrustworthy rather than deriving a wrong MSS from them.

**Clock discipline**

Timestamps are how captures from different volunteers get correlated. An unsynced clock makes a
Tier 3 dual-client capture worthless.

```powershell
# Administrator:
w32tm /resync
w32tm /query /status          # record 'Last Successful Sync Time' and 'Phase Offset'
```

If the service is not running, `doctor` says so and prints `net start w32time`. `sccap` records
clock anchors from both a wall-clock and a monotonic source in every session, so a mid-session
step is visible rather than silently folded into your timings.

**Quiesce the host**

The correct capture filter is *no capture filter* (§1.5), which means everything else on the
machine lands in your file. Before every session, close: web browsers, Discord, OneDrive/Dropbox,
game launchers other than Steam, and anything that syncs. Pause Windows Update.

```powershell
# See what is actually talking, busiest first:
Get-NetTCPConnection -State Established |
    Select-Object RemoteAddress, RemotePort, OwningProcess |
    Sort-Object OwningProcess

# Resolve the noisy ones to names:
Get-Process -Id (Get-NetTCPConnection -State Established).OwningProcess |
    Select-Object -Unique ProcessName
```

> **Do NOT close Steam, and do NOT filter out Steam traffic.** If the client authenticates via a
> Steam session ticket, that exchange is part of the auth flow the emulator must service. Steam
> traffic is in scope.

### 1.3 Phase 0 — Endpoint discovery

You cannot write a correct filter for an application whose endpoints you have not measured. Do
not copy port numbers from forum posts; derive them.

1. **Find the interface first.** This is the one step that is both mandatory and cheap. With the
   game running and sitting in the hangar:

   ```powershell
   .\out\sccap.exe doctor --watch 30s
   ```

   It samples every live adapter simultaneously using the same capture backend a real session
   would, and reports which of them actually carries traffic to the game's ports:

   ```
    * Ethernet     packets=1841    game=212    services=shard(180),chat(32)
      Wi-Fi        packets=12      game=0
      vEthernet    packets=0       game=0
   ```

   The starred line is the one to record. **If nothing shows game traffic, stop.** Capturing anyway
   produces a file that passes every other check in this document and contains none of the game.
   It is the only failure here that is both silent and total.

2. **Find the process and its sockets.** The game is one process — no launcher tree to chase, no
   compatibility layer owning the socket on its behalf.

   ```powershell
   Get-Process StarConflict | Select-Object Id, Path, StartTime

   # TCP endpoints, live:
   Get-NetTCPConnection -OwningProcess (Get-Process StarConflict).Id |
       Select-Object LocalPort, RemoteAddress, RemotePort, State

   # UDP is connectionless, so only the local port is visible here:
   Get-NetUDPEndpoint -OwningProcess (Get-Process StarConflict).Id |
       Select-Object LocalAddress, LocalPort
   ```

   Sample this repeatedly while playing through login → hangar → matchmaking → a match → back to
   hangar. Endpoints appear and disappear across those transitions, which is the point.

3. **Recover UDP peers from the capture itself.** The local port above is only half the pair. The
   remote match-server endpoints come out of the recording:

   ```powershell
   .\out\sccap.exe decode .\captures\SC_* --type SCMD_CONNECT_DEDICATED_SERVER
   ```

   That message is the handoff: it hands the client the address, port and session id of a match
   server. It is the single most valuable message in the protocol, because everything after it is
   traffic no tool in this project has ever recorded.

   With Wireshark installed, the same thing from the raw file:

   ```powershell
   & "$env:ProgramFiles\Wireshark\tshark.exe" -n -r .\captures\SC_*\capture_00001_*.pcapng `
       -Y "udp.port == <LOCAL_UDP_PORT>" -T fields -e ip.src -e ip.dst -e udp.srcport -e udp.dstport
   ```

4. **Commit the result to the pooled endpoint inventory** (§5.2). Every volunteer's endpoints go
   into one list, which becomes the emulator's DNS/route redirect map.

**Expected shape (a hypothesis to verify, not a fact to assume):** a TLS/HTTPS auth and
launcher/CDN phase on TCP 443, a persistent TCP control channel to a lobby/master server, and a
separate UDP flow to a match server whose address is handed to the client at matchmaking time.
Your Phase 0 output either confirms this or — more valuably — contradicts it. Report
contradictions.

### 1.4 Isolating the game's traffic — what is and is not available here

There is no way on Windows to give the game a private network stack that you then capture in
isolation. On other platforms that trick exists and removes the largest source of error in this
protocol; here it does not, and pretending otherwise would waste your time.

**So the answer is quiescing, not isolation, and it is done at §1.2.** Your capture will contain
whatever else the machine was doing. That is acceptable — Principle I says capture everything and
decide later, and a slightly noisy file is vastly better than a filtered one — but it puts real
weight on closing things beforehand.

Three things make the noise manageable after the fact:

- **The recording is complete, so filtering stays reversible.** Everything below is read-time
  filtering against a full archive, which is allowed and safe. `sccap decode --type`,
  `--status`, and Wireshark display filters (§1.6) all narrow the view without touching the
  evidence.
- **`sccap` identifies the game's own flows.** It knows the master-server ports and surfaces them
  in the status line and the record index, so "which of these is the game" is answered for you
  rather than guessed.
- **The marker beacon (§2.5) anchors the timeline**, so you can find the interesting thirty
  seconds in an hour of file without knowing anything about the traffic around it.

If you want genuinely isolated traffic, the mirror-port tap in §0.4 is the way to get it: capture
between the gaming machine and the router and the file contains that machine's traffic and nothing
from your capture host at all. Set `host.tap: true` in the sidecar.

**Verify before trusting a session.** Capture for ten seconds with the game idle in the hangar and
look at what landed:

```powershell
.\out\sccap.exe capture --scenario BASE-03 --out .\captures    # Ctrl+C after ~10s
.\out\sccap.exe verify .\captures\SC_*
```

If `drops` is anything but 0, the machine is too busy — close more and try again before recording
anything you care about.

### 1.5 Capture filters (BPF, applied at capture time)

> **Default policy: capture with NO capture filter.**
> A capture filter is irreversible. Anything it drops is gone forever, and the traffic most
> likely to be dropped by a naive filter is exactly the traffic nobody predicted — a NAT
> punch-through to an unexpected subnet, a telemetry channel, a CDN fetch, a fallback relay.
> Take the disk hit.

`sccap` has no capture-filter flag and will not grow one — capture-time filtering is a permanent,
irreversible discard decision (Principle I). If you are genuinely disk-constrained, the honest
lever is `--floor`, which stops the session cleanly with the metadata written rather than
quietly dropping traffic to fit.

For reference, a filter that removes only LAN service chatter definitively not the game, and
**explicitly preserves the marker beacon**, would look like this. It is documented so you can
recognise one in somebody else's pre-`sccap` capture, not so you can apply it:

```
udp port 24242 or not (
     tcp port 445 or tcp port 139 or udp port 137 or udp port 138
  or udp port 5353 or udp port 5355 or udp port 1900 or udp port 3702
  or udp port 17500 or udp port 57621
)
```

Removes: SMB/CIFS, NetBIOS, mDNS/Avahi, LLMNR, SSDP, WS-Discovery, Dropbox LAN sync, Spotify LAN
discovery. Preserves: DNS, ICMP, ARP, DHCP, NTP, all TCP/UDP game candidates, and SCMARK.

**Never put these in a capture filter:**

| Do not exclude | Reason |
|---|---|
| `port 53` (DNS) | DNS responses are the primary source of the server hostname inventory. Essential. |
| ICMP | Port-unreachable and fragmentation-needed messages diagnose MTU and teardown behaviour. |
| ARP | Cheap, and needed to interpret link-layer changes and gateway behaviour. |
| Steam ports | May carry the auth ticket (§1.2). |
| Broadcast | Kills the SCMARK sync beacon and with it your video correlation. |
| Anything by *guessed* game port | You will be wrong and you will not find out until analysis. |

### 1.6 Display filters (applied at analysis time — always reversible)

Build an inclusive filter from the endpoints you measured in §1.3:

```
ip.addr in {203.0.113.10, 203.0.113.11} || udp.port in {31337, 31338} || tcp.port in {443, 5222}
```

Working filters for triage:

```wireshark
# Isolate the sync markers, readable as text in the Bytes pane
udp contains "SCMARK"

# All game traffic carrying an actual payload (drops bare ACKs and empty keepalives)
ip.addr in {<GAME_IPS>} && (tcp.len > 0 || udp.length > 8)

# The realtime channel only, server -> client
udp && ip.src in {<MATCH_SERVER_IPS>} && udp.length > 8

# The realtime channel only, client -> server (your input/command stream)
udp && ip.dst in {<MATCH_SERVER_IPS>} && udp.length > 8

# The control channel during a match (cross-channel correlation, see §3.6)
tcp && ip.addr in {<LOBBY_IPS>} && tcp.len > 0

# TLS handshakes only -- extracts the hostname inventory
tls.handshake.type == 1 || tls.handshake.type == 11

# Everything that is NOT the game (sanity check: what noise got in?)
!(ip.addr in {<GAME_IPS>}) && !(arp || dns || icmp || udp contains "SCMARK")

# Find your seeded known-plaintext anchor (see §2.7)
frame contains "ZZQQ"

# Exclude LAN noise (you captured unfiltered, so it is all in there)
!(mdns || llmnr || nbns || ssdp || dhcp || igmp)

# Windows-specific noise that shows up on a normal desktop
!(nbns || browser || wsdd || dhcpv6)
```

Filtering by IP is strictly better than filtering by application name. An **inclusive** filter
built from Phase 0 endpoints is exact; name-based exclusion of browsers and chat clients is not.

### 1.7 Wireshark profile settings

Create a dedicated profile (**Edit → Configuration Profiles → +**, name it `star-conflict`), then
share `%APPDATA%\Wireshark\profiles\star-conflict\` with your first submission so others can
drop it straight in.

| Setting | Value | Why |
|---|---|---|
| Protocols → TCP → *Validate the TCP checksum if possible* | **Off** | Checksum offload marks valid outbound packets as bad and hides them from filters. This is not optional on Windows — checksum offload is on by default and is not one of the things §1.2 tells you to disable. |
| Protocols → TCP → *Allow subdissector to reassemble TCP streams* | **On** | Needed to see application PDUs spanning segments. |
| Protocols → TCP → *Reassemble out-of-order segments* | **On** | |
| Protocols → UDP → *Try heuristic sub-dissectors first* | **Off** | Stops Wireshark mis-dissecting the game protocol as RTP and hiding the raw bytes. |
| View → Name Resolution → *Resolve network addresses* | **Off** | Prevents Wireshark's own DNS lookups polluting a live capture. |
| Time Display Format | **UTC date and time of day**, precision **Milliseconds** | Correlates with markers and video. Never use "Seconds Since Beginning of Capture" for archival work. |

Custom columns (Preferences → Columns): `frame.time_utc`, `frame.time_delta_displayed`,
`udp.length`/`tcp.len`, `udp.stream`/`tcp.stream`.

### 1.8 Capture invocation

**Use `sccap`, never the Wireshark GUI, for real captures.** The GUI dissects live, which costs
CPU and causes driver-level packet drops under load — exactly during combat, exactly when you
need the data.

```powershell
.\out\sccap.exe capture --scenario ECON-03 --volunteer vol-042 --region EU --out .\captures
```

That creates the correctly-named bundle, writes `session.json` before the first frame is
captured, identifies the game client automatically, and starts journalling. Add
`--interface Ethernet` if §1.3 showed more than one live adapter.

The defaults are the settings that matter, and they are not adjustable by accident:

| Default | Why it is this way |
|---|---|
| **Full snaplen** — no truncation | The payload is the entire point. A truncated capture is unusable, and this is the single most common fatal mistake in packet archival. `--snaplen` exists but records a warning into the bundle, and `verify` reports it. |
| **No capture filter** | A capture filter is an irreversible discard decision (§1.5, Principle I). Read-time filtering against a complete archive is what you want instead. |
| **64 MB buffer, immediate mode** | Sized for combat bursts. Immediate mode rather than batching, so the drop counter is truthful within a second rather than a batch. |
| **Rolls segments by size and duration, never deletes** | Files stay navigable and uploadable. There is no ring buffer and no `--prune`: nothing ever deletes your oldest data to make room. |

**Watch the status line. `drops` must stay at 0:**

```
[00:04:12] services=lb,shard,chat  frames=182401  journal=241MiB  drops=0  records=9188  novel=2
```

A climbing `drops` means traffic crossed the wire and is missing from your file. Stop, close
things, start again. It is on screen precisely so you find out in ten seconds rather than after an
hour. `novel` counting up is good — it means you have hit something the tool has never seen.

Afterwards, the drop count is checked again from the recorded metadata rather than from memory:

```powershell
.\out\sccap.exe verify .\captures\SC_*
```

### 1.9 TLS / SSL session key extraction

The goal is an **NSS key log** that lets Wireshark decrypt the auth and launcher traffic.

**Be warned: this is the hardest section in the manual, and it may not be possible on your
machine.** Read Step 5 below before you spend an evening on it — the fallback is genuinely fine.

**Why it is hard here.** Windows applications that use the operating system's own TLS
implementation (SChannel) cannot be persuaded to write a key log. `SSLKEYLOGFILE` is a convention
honoured by OpenSSL, NSS and GnuTLS; SChannel ignores it completely, and there is no supported
switch that changes that. Whether this route works at all therefore depends entirely on what the
client links against, which is what Step 1 establishes before you try anything.

This is a real cost of recording on the platform the game ships for, and it is worth being clear
that it is a cost. What you get in exchange is that everything *else* in the capture — the TCP
behaviour, the handshake fingerprint, the timing — is the game's own rather than a compatibility
layer's. The master-server protocol, which is the bulk of the archive and the part with no
encryption at all, is unaffected either way.

#### Step 1 — Identify the TLS stack, before trying anything

```powershell
# Which crypto libraries has the running client actually loaded?
Get-Process StarConflict -Module |
    Where-Object { $_.ModuleName -match 'ssl|crypto|nss|gnutls|schannel|ncrypt|bcrypt' } |
    Select-Object ModuleName, FileName
```

| Modules loaded | Implication |
|---|---|
| `libssl*.dll` / `libcrypto*.dll` | OpenSSL, bundled with the game. `SSLKEYLOGFILE` has a real chance — go to Step 2. |
| `nss3.dll` / `ssl3.dll` | NSS. `SSLKEYLOGFILE` works. Go to Step 2. |
| `schannel.dll` / `ncrypt.dll` / `bcrypt.dll` only | The operating system's stack. `SSLKEYLOGFILE` will not work. Skip to Step 3. |
| None, but TLS is on the wire | Statically linked or custom crypto. Skip to Step 3. |

Record the finding in `session.json` → `tls.stack_detected`. **This is a useful data point even
when extraction fails** — knowing the client uses SChannel tells an emulator author what the
server must be prepared to negotiate with.

#### Step 2 — Set `SSLKEYLOGFILE` (only if Step 1 found a bundled library)

Steam passes its own environment to the game, so set it for the whole session before launching
Steam:

```powershell
New-Item -ItemType Directory -Force -Path "$HOME\sc-archive" | Out-Null
[Environment]::SetEnvironmentVariable(
    'SSLKEYLOGFILE', "$HOME\sc-archive\sslkeys.log", 'User')
```

Then **fully exit Steam** (right-click the tray icon → Exit; not just the window) and start it
again, so it picks up the new environment. Launch the game and reach the hangar.

Verify that keys are actually being written — do not assume:

```powershell
Get-Content "$HOME\sc-archive\sslkeys.log" -TotalCount 2
(Get-Content "$HOME\sc-archive\sslkeys.log").Count
```

A working key log contains lines beginning with `CLIENT_RANDOM` (TLS 1.2) or
`CLIENT_HANDSHAKE_TRAFFIC_SECRET` / `CLIENT_TRAFFIC_SECRET_0` / `SERVER_TRAFFIC_SECRET_0`
(TLS 1.3). **An empty or absent file means this route does not work for this client.** Record the
result either way in `session.json` → `tls`.

Unset it afterwards, so it does not sit in your environment for every other application:

```powershell
[Environment]::SetEnvironmentVariable('SSLKEYLOGFILE', $null, 'User')
```

#### Step 3 — Intercepting proxy (last resort)

Only if Steps 1–2 established that no key log is obtainable.
[PolarProxy](https://www.netresec.com/?page=PolarProxy) writes the *decrypted* traffic straight to
a PCAP, which is exactly the archival artifact we want:

```powershell
.\PolarProxy.exe -p 443,80 -x rootCA.cer -o .\decrypted_pcaps\
```

Three caveats, all of which must go in the sidecar:

- **Certificate pinning defeats this.** If the client errors during login with the proxy active,
  it pins. Set `tls.pinning_suspected: true` and fall back to capturing ciphertext.
- **A proxy changes the wire-level endpoints**, so always take a matching un-proxied capture of
  the same scenario so the true topology is preserved somewhere.
- **Installing a root CA on your own machine is a real change to your machine's trust store.**
  Remove it when you are done, and do not do this on a machine you use for anything sensitive.

#### Step 4 — Bind the keys to the capture, permanently

Keys are useless if separated from their PCAP, and volunteer bundles get shuffled. Inject the
secrets **into** the pcapng as a Decryption Secrets Block so they can never be lost:

```powershell
& "$env:ProgramFiles\Wireshark\editcap.exe" --inject-secrets tls,"$HOME\sc-archive\sslkeys.log" `
    .\capture_00001.pcapng .\capture_00001_keyed.pcapng
```

Verify by opening the `_keyed` file with no key preference configured — if application data
appears decrypted, it worked. Ship **both** the keyed file and the raw `sslkeys.log`, and set
`tls.injected_into_pcapng: true`.

> Keep the original file too, and do not overwrite it. The unmodified segment is the evidence and
> its hash is in `SHA256SUMS`; the keyed file is derived, and `verify` will report it as an extra
> file rather than a corruption.

#### Step 5 — Even if all of this fails, capture anyway

**This is the important step, and it is the one most likely to be skipped in frustration.**

Encrypted bytes plus the client binary are still a solvable problem — the key schedule lives in
the executable, which is why §6.1 tells you to archive it. An *uncaptured* handshake is
unsolvable forever. **Never skip a scenario because you could not decrypt it.**

Keep this in proportion: the master-server protocol — three TCP services carrying authentication,
inventory, economy, matchmaking and chat, which is the overwhelming majority of what this project
is trying to preserve — **has no transport encryption at all.** None of the above applies to it.
TLS covers the web-facing auth and launcher flow, one valuable corner of a much larger archive.

Two things to extract regardless of decryption, because both feed the emulator's redirect map:

```powershell
$tshark = "$env:ProgramFiles\Wireshark\tshark.exe"

# Every hostname the client asked for (SNI)
& $tshark -n -r .\capture_00001.pcapng -T fields -e tls.handshake.extensions_server_name |
    Where-Object { $_ } | Sort-Object -Unique

# Every hostname resolved, with answers
& $tshark -n -r .\capture_00001.pcapng -Y "dns.flags.response==1" `
    -T fields -e dns.qry.name -e dns.a | Sort-Object -Unique
```

### 1.10 Mirror-port capture (Tier 3, optional)

Use when you want the best possible wire fidelity: frames untouched by the gaming machine's NIC
offloads, and zero capture-induced load on the client. **No capture software runs on the gaming
machine at all.**

Two ways to get there, in order of preference:

**A managed switch with port mirroring.** Mirror the gaming machine's port to a spare port,
connect a second Windows machine to it, and capture there. Nothing about the game's path changes
— this is a pure observer.

**A second machine with two NICs, bridging.** The gaming machine plugs into it; it forwards to the
router. Windows can bridge two adapters directly:

```powershell
# On the capture machine, as Administrator. Both adapters, one bridge:
New-NetSwitchTeam -Name "SCBridge" -TeamMembers "Ethernet","Ethernet 2"
```

Then capture on the bridge interface with `sccap` exactly as normal:

```powershell
.\out\sccap.exe doctor --watch 30s          # confirm the game's traffic appears here
.\out\sccap.exe capture --scenario CBT-01 --interface "SCBridge" --out .\captures
```

Set `host.tap: true` in the sidecar so a reader knows the frames were observed off-box, and record
which arrangement you used in `notes.md` — a mirror port and a bridge have different failure
modes and it matters which one produced the file.

Run the marker beacon (§2.5) **on the gaming machine**, so its broadcast datagrams traverse the
tap and land in the capture alongside the game's traffic. `sccap mark` on the capture host would
stamp the log but its packets would never cross the mirrored link, and you would lose
frame-accurate video sync — if you have to do it that way, record
`video.marker_overlay: false` and say so in `notes.md`.

---

## 2. The Modular Capture Protocol

### 2.1 The bundle is the unit of submission

Never submit a bare `.pcapng`. The unit of work is a **bundle directory** containing the capture
plus everything needed to interpret it.

```
SC_20260814T203015Z__AUTH-02__vol-042__EU__000/
├── capture_00001_20260814203015.pcapng   # the evidence (may be several segments)
├── capture_00002_20260814204015.pcapng
├── session.json                          # REQUIRED -- the sidecar, see §2.4
├── markers.log                           # REQUIRED -- SCMARK marker log
├── index.jsonl                           # derived -- one line per decoded record
├── coverage-delta.json                   # derived -- what this session contributed
├── sockets.txt                            # T2 -- endpoint inventory from §1.3
├── display-filter.txt                    # T2 -- Wireshark filter built from it
├── sslkeys.log                           # T2 -- if TLS extraction succeeded (§1.9)
├── video.mkv                             # REQUIRED for gameplay scenarios
├── notes.md                              # anything surprising that happened
└── SHA256SUMS                            # REQUIRED -- generated last
```

`sccap` writes everything above except `video.mkv`, `notes.md` and the two optional T2 files, and
it writes `session.json` **before the first frame is captured** so that even a session killed one
second later is self-describing. The captured and dropped counts live in `session.json` →
`host.packets_captured` / `packets_dropped`; there is no separate tool log to keep.

A bundle missing `session.json` or `markers.log` will be rejected at intake. This is not
bureaucracy: without them the capture cannot be placed on a timeline and cannot be correlated
with any other volunteer's data.

Only the `.pcapng` segments and `session.json` are irreplaceable. `index.jsonl` and
`coverage-delta.json` are derived and can be regenerated from the segments at any time with
`sccap index --rebuild`, including years from now by a better decoder.

### 2.2 Naming

```
SC_<UTC_START>__<SCENARIO_ID>__<VOLUNTEER_ID>__<REGION>__<SEQ>
```

| Field | Format | Example |
|---|---|---|
| `UTC_START` | `YYYYMMDDThhmmssZ`, **UTC always** | `20260814T203015Z` |
| `SCENARIO_ID` | From §3, without the `SC-` prefix | `AUTH-02`, `CBT-07` |
| `VOLUNTEER_ID` | Pseudonymous, assigned at signup | `vol-042` |
| `REGION` | Server region you connected to | `EU`, `NA`, `RU`, `SEA` |
| `SEQ` | Zero-padded attempt counter | `000`, `001` |

No spaces, no colons, no local time, ASCII only. `sccap capture` generates this from
`--scenario`, `--volunteer` and `--region` — use it rather than naming by hand, and note that it
picks the next free `SEQ` rather than ever overwriting an existing bundle.

### 2.3 Segmentation — how to avoid submitting a 4-hour blob

**The rule: one scenario, one bundle.** Start the capture immediately before the scenario, stop
it immediately after. Do not leave a capture running across scenarios.

Every capture is wrapped in this envelope:

1. **Start** `sccap capture` in an Administrator terminal.
2. **10 seconds of deliberate idle.** Touch nothing. This lead-in captures the steady state
   immediately before your action, which is what the diff is taken against.
3. **Stamp a marker** naming the scenario, from a *second* Administrator terminal:
   `sccap mark "BEGIN AUTH-02"`.
4. **Perform the scenario**, stamping a marker before *and* after each atomic sub-action.
   `sccap mark --console` keeps a prompt open for this — type a label, press Enter, repeat.
5. **Stamp** `END AUTH-02`.
6. **10 seconds of deliberate idle** lead-out.
7. **Stop** the capture with **Ctrl+C**.

`sccap mark` finds the running capture by itself — there is no session id to pass and no beacon to
start or stop separately.

Hard limits:

- **Soft cap 10 minutes / 200 MB per segment**, rolled automatically. Segments accumulate; nothing
  is ever deleted to make room.
- **Hard cap 30 minutes per bundle** except a full match or Open Space session, which get their
  own scenario IDs.
- **If something unplanned happens** — do not delete the capture. Stamp
  `sccap mark "ANOMALY <what happened>"`, finish the envelope, note it in `notes.md`, and submit
  it flagged. Unplanned events are frequently the most informative captures in the archive.

**Never:** run one capture across login → match → logout; stop and restart mid-scenario; or edit
and re-save a PCAP before submitting. Filtering is the dev team's job, is destructive when done
wrong, and `verify` will notice the hash no longer matches.

> **Closing the console window is not stopping the capture.** Windows gives the process about two
> seconds and then terminates it. The bundle survives that — it is designed to, and §4 will report
> it as `VERIFIED (interrupted)` rather than failing it — but you lose the clean close and the
> final `SHA256SUMS`. Press Ctrl+C.

### 2.4 The session sidecar (`session.json`)

Machine-readable metadata. `sccap capture` writes it before the first frame lands and refreshes
it periodically, so an abruptly killed session is still self-describing. Everything under
`clock`, `client`, `host`, `markers` and the schema/version fields is filled in automatically —
you add the human knowledge: `account`, `economy_ledger`, `known_plaintext`, `video` and
`notes`. `sccap verify` reports anything inconsistent with what is actually on disk.

```json
{
  "schema_version": "1.0",
  "bundle_id": "SC_20260814T203015Z__ECON-03__vol-042__EU__000",
  "scenario_id": "SC-ECON-03",
  "volunteer_id": "vol-042",
  "region": "EU",
  "tier": 2,
  "utc_start": "2026-08-14T20:30:15.000Z",
  "utc_end":   "2026-08-14T20:38:02.000Z",

  "clock": { "ntp_source": "time.windows.com", "offset_ms_at_start": -3.2,
             "method": "w32tm /query /status" },

  "client": {
    "name": "Star Conflict", "build_id": "24666578",
    "depot_manifests": { "212072": "3741217375342066373" },
    "install_path": "C:\\Program Files (x86)\\Steam\\steamapps\\common\\star conflict",
    "binary_name": "StarConflict.exe",
    "binary_sha256": "830f9e5b21c9612d...",
    "binary_build_id": "3F2A9C10BE4411D7A9F30060B0EC3D3901",
    "binary_build_id_kind": "codeview",
    "binary_arch": "I386 (32-bit)",
    "platform": "windows", "runtime": "native", "launcher": "steam", "locale": "en"
  },

  "host": {
    "os": "Windows 11 26100.2314", "link": "wired-1000M", "nic": "Intel I225-V",
    "interface": "Ethernet", "tap": false,
    "offloads_disabled": ["lso", "rsc"],
    "capture_tool": "sccap 0.4.0 (npcap)", "capture_filter": "", "snaplen": 0,
    "packets_captured": 184203, "packets_dropped": 0
  },

  "account": { "throwaway": true, "state": "mid-progression", "max_ship_rank": 9, "has_dlc": false },

  "economy_ledger": [
    { "utc": "2026-08-14T20:31:40.100Z", "marker_seq": 42, "event": "before_purchase",
      "credits": 1450320, "gs": 1200, "monocrystals": 87 },
    { "utc": "2026-08-14T20:31:52.700Z", "marker_seq": 43, "event": "after_purchase",
      "credits": 1201320, "gs": 1200, "monocrystals": 87,
      "item": "Mk3 Shield Booster", "price": 249000, "currency": "credits" }
  ],

  "known_plaintext": { "callsign": "ZZQQXX01",
                       "seeded_strings": ["ZZQQXX01", "AAAAAAAA0001", "QQQQ-LOADOUT-7"] },

  "markers": { "beacon_port": 24242, "count": 96, "first_seq": 1, "last_seq": 96 },
  "video": { "file": "video.mkv", "fps": 60, "start_utc": "2026-08-14T20:30:12.400Z",
             "marker_overlay": true },
  "tls": { "keylog_present": false, "keylog_lines": 0, "injected_into_pcapng": false,
           "stack_detected": "schannel", "pinning_suspected": false, "proxy_used": "none" },

  "anomalies": [],
  "notes": "Third repetition of the same purchase; see notes.md."
}
```

The `economy_ledger` is worth the typing. Exact before/after integers let a developer search the
payload for the little-endian encoding of `1450320` and `1201320` and land directly on the
currency field. This converts hours of blind structural analysis into a `grep`.

### 2.5 Video ↔ packet synchronization: the SCMARK beacon

Wall-clock comparison between OBS and Wireshark is not good enough — OBS's recording start
timestamp, encoder latency, and container timebase collectively introduce hundreds of
milliseconds of unknown skew, which at a 30 Hz snapshot rate is a dozen ambiguous ticks.

**The solution is a marker that appears in both media simultaneously.** `sccap` broadcasts a
small ASCII UDP datagram onto the local link once per second, and again whenever you stamp one:

```
SCMARK|000042|2026-08-14T20:31:40.118Z|884421993310|EVENT|purchase Mk3 Shield Booster
```

Because it is broadcast on the same interface the game uses, **it is captured inline in the
PCAP, interleaved with game packets, timestamped by the same capture clock.** The same line is
printed to a console window, which you capture in the corner of your screencast.

This gives a rigid three-way binding with no clock assumptions at all:

```
video frame  ──shows──►  seq 000042  ◄──contains──  PCAP frame #58211
```

To align, a developer reads the sequence number on-screen at the moment of interest and filters
`udp contains "SCMARK"` for it. Alignment error is bounded by one video frame, not by clock skew.

**Operation**

Leave a second Administrator terminal open beside the capture:

```powershell
.\out\sccap.exe mark --console
# Heartbeats emit automatically once per second.
# Type a label and press Enter to stamp an EVENT marker:
BEGIN ECON-03
before purchase  credits=1450320
after purchase   credits=1201320
END ECON-03
# Ctrl+Z then Enter to finish.
```

One-off marks without a console:

```powershell
.\out\sccap.exe mark "ANOMALY the client froze for 5s"
```

`sccap mark` locates the running capture through a pointer under `%LOCALAPPDATA%\sccap`, so there
is no session id to pass and nothing to keep in sync. If no capture is running it says so rather
than writing a marker nobody will find.

> **Windows Firewall can silently eat the beacon.** The marker line is always written to
> `markers.log` — that record is durable and never depends on the network — but the *datagram*
> is what lands in the PCAP and gives you video sync. If §2.5's verification below finds zero
> markers in the capture, allow `sccap.exe` outbound UDP, or accept the prompt Windows raises the
> first time you run a capture.

Verify markers survived into the capture:

```powershell
.\out\sccap.exe decode .\captures\SC_* --type SCMARK
```

Or, independently, with Wireshark:

```powershell
& "$env:ProgramFiles\Wireshark\tshark.exe" -n -r .\captures\SC_*\capture_00001*.pcapng `
    -Y 'udp contains "SCMARK"' -T fields -e frame.number
```

Zero means the beacon datagrams were firewalled or the wrong interface was captured. Fix it and
recapture — a gameplay bundle without markers in the PCAP has no video correlation.

### 2.6 Screencast configuration

**OBS Studio** ([obsproject.com](https://obsproject.com/), or `winget install OBSProject.OBSStudio`).

| Setting | Value | Why |
|---|---|---|
| Container | **MKV** | Survives a crash mid-recording; MP4 does not. Remux afterwards if you like. |
| Resolution | Native, no downscale | HUD numbers must be legible — they are the ground truth for damage, currency, and speed. |
| FPS | **60** | 30 is acceptable but halves your temporal resolution against a 30 Hz snapshot stream. |
| Encoder | **NVENC** (NVIDIA), **AMF** (AMD) or **QuickSync** (Intel) | Software x264 steals CPU from the capture and causes packet drops. |
| Rate control | CQP ~20 | Quality matters more than file size; a low bitrate smears HUD text. |
| Audio | **On** | Free extra sync channel; weapon and hit audio cues time-align to combat events. |

**Capture mode.** Use **Game Capture** for the game itself and add a **Window Capture** source for
the marker console. Display Capture works too and costs slightly more GPU. If Game Capture shows
a black screen, run OBS as Administrator — it needs equal or greater privilege than the process it
is hooking, and your capture terminal is already elevated.

**Required on-screen elements**, arranged so they never overlap the HUD:

1. The **marker console** (small window, bottom-right, always-on-top) so the sequence number is
   readable in every frame.
2. A **UTC clock with milliseconds** — belt and braces if the beacon dies mid-session:
   ```powershell
   while ($true) { Write-Host -NoNewline "`r$((Get-Date).ToUniversalTime().ToString('HH:mm:ss.fff'))"; Start-Sleep -Milliseconds 100 }
   ```
3. Any **HUD element the scenario measures**: currency counters for economy, speed/throttle for
   physics, damage numbers for combat.

Start OBS **before** the capture, stop it **after**. Record `video.start_utc` in the sidecar.

### 2.7 Known-plaintext seeding

Do this once per throwaway account, before capturing. It costs five minutes and materially
accelerates analysis.

| Where | Seed with | Reveals |
|---|---|---|
| Player callsign / nickname | `ZZQQXX01` | String encoding (UTF-8/UTF-16), length prefix width, your own player-record offset |
| Custom loadout / preset name | `QQQQ-LOADOUT-7` | Loadout struct layout |
| Squad / corporation chat | `A`, then `AA`, `AAAA`, `AAAAAAAA`, `A`×16, `A`×32 | **The single best oracle.** A monotonic length ladder reveals the length-prefix field, its width and endianness, and whether payloads are compressed or encrypted — if a 1-char and a 32-char message differ by exactly 31 bytes at a fixed offset, the channel is plaintext-framed; if unpredictably, it is compressed or encrypted. |
| Ship name (if renamable) | `ZZQQ-SHIP-03` | Entity naming and per-ship record layout |

Use ASCII, a restricted alphabet (`Z`, `Q`, `X`, `A`), and strings unlikely to occur naturally, so
they survive a byte search and stay recognisable under simple transforms. Record every seeded
string in `known_plaintext.seeded_strings`.

### 2.8 Bonus annotation channels (Tier 2/3, optional)

Neither replaces a PCAP, but both give a second, independent view that makes ambiguous captures
decidable. Run either alongside a normal capture, never instead of one.

**Socket-level correlation.** Sample the client's endpoints on a tight loop while playing, so
API-level intent can be lined up against wire bytes:

```powershell
while ($true) {
    $t = (Get-Date).ToUniversalTime().ToString('o')
    Get-NetTCPConnection -OwningProcess (Get-Process StarConflict).Id -ErrorAction SilentlyContinue |
        ForEach-Object { "$t,TCP,$($_.LocalPort),$($_.RemoteAddress),$($_.RemotePort),$($_.State)" }
    Start-Sleep -Milliseconds 250
} | Tee-Object -FilePath .\sockets.txt
```

Keep `sockets.txt` in the bundle. It is how a reader tells which of several concurrent flows the
client considered established at a given moment.

**ETW network tracing** — a kernel-level record of every send and receive the process made, with
timestamps, independent of anything this project wrote:

```powershell
# Administrator. Produces an .etl alongside the capture.
netsh trace start capture=no provider=Microsoft-Windows-TCPIP level=5 tracefile=.\tcpip.etl
# ...play...
netsh trace stop
```

This costs real CPU. Never run it during a physics or hit-registration scenario where timing
fidelity matters; it is for hangar and economy work, where it resolves "did the client send that,
or did the server volunteer it?"

---

## 3. Step-by-Step Scenario Checklist

**Format.** Every scenario states **Do** (exact player actions), **Capture** (envelope and
required artifacts), and **Look for** (what the developer will extract — included so you
understand why precision matters, and so you can tell when a capture went wrong).

**Universal preconditions for every scenario below:**
- [ ] `sccap doctor` shows no `FAIL`, in an Administrator terminal
- [ ] `sccap doctor --watch 30s` confirmed the interface actually carries game traffic
- [ ] LSO and RSC checked (§1.2), clock synced
- [ ] Capture started with `sccap capture` — full snaplen and no filter are the defaults
- [ ] `sccap mark --console` running in a second terminal, visible in the screencast
- [ ] OBS recording, HUD elements visible
- [ ] 10 s idle lead-in and lead-out
- [ ] `drops=0` held for the whole session
- [ ] Sidecar completed afterwards; `sccap verify` passes

`P0` marks a triage priority from §0.6.

---

### 3.0 Baselines — capture these first, they are cheap and make everything else readable

#### SC-BASE-01 — Hangar idle baseline `P0`
**Do.** Log in, arrive at the hangar, stamp `BEGIN BASE-01`, then **touch nothing at all for 120
seconds** — no mouse movement over UI elements, no menu navigation. Stamp `END BASE-01`.

**Capture.** Full envelope. Video optional but preferred (proves you really were idle).

**Look for.** The lobby keepalive/heartbeat cadence and payload; the minimum viable "connection
is alive" exchange the emulator must implement; whether the server pushes unsolicited state
(players online, chat presence, timers) while idle; TCP keepalive vs application-level ping.

#### SC-BASE-02 — In-match idle baseline `P0`
**Do.** Enter a **Practice / PvE match** (no other humans). At spawn, stamp `BEGIN BASE-02`, then
**release all controls completely for 60 seconds**. Do not touch mouse or keyboard. Stamp
`END BASE-02`. Repeat once while stationary in an empty area away from any NPC.

**Capture.** Full envelope, video **required**.

**Look for.** The realtime snapshot tick rate; the size and shape of a "null" delta-compressed
snapshot; the client→server input packet rate when input is neutral; the baseline
sequence-number/ACK fields, trivially visible when nothing else in the packet is changing.
**This capture is what makes every combat capture interpretable.**

Measure the tick rate from the result:
```bash
tshark -n -r capture_00001*.pcapng -Y "ip.src==<MATCH_SERVER> && udp" -q -z io,stat,0.05
tshark -n -r capture_00001*.pcapng -Y "ip.src==<MATCH_SERVER> && udp" \
       -T fields -e frame.time_delta_displayed | sort -n | uniq -c | sort -rn | head
```

#### SC-BASE-03 — Client launched, never logged in
**Do.** Start the capture, launch Steam/the client, **stop at the login screen**, wait 60 s, quit.

**Look for.** Launcher/CDN/patcher endpoints, version-check exchange, the pre-auth handshake, and
the complete hostname inventory before any credentials are involved. Safe to submit publicly with
minimal review — no session token present.

---

### 3.1 Authentication (TCP / TLS focused)

#### SC-AUTH-01 — Brand-new account: first ever login and tutorial `P0` `IRREPLACEABLE`
**Do.**
1. Create a **fresh throwaway account** out-of-band, before starting the capture (registration
   involves an email address and a password you type).
2. Seed the callsign per §2.7 during creation if possible.
3. Start the capture envelope, then log in for the very first time.
4. Stamp markers at each stage: `first login`, `tutorial start`, each tutorial objective,
   `tutorial complete`, `first hangar arrival`, `starter ship received`.
5. Play the entire first-run experience through to a stable hangar. Do not skip the tutorial.
6. Record starting currency values in `economy_ledger`.

**Capture.** Full envelope, video required, TLS keys if available. Expect this to exceed the
30-minute soft cap — allowed for this scenario. Do **not** split it.

**Look for.** The pristine, minimal account state — the emulator's `CREATE ACCOUNT` template.
Every default: starting ships, credits, unlocked slots, faction standings, tutorial flags.
Tutorial scripting is server-driven in most MMOs, so this also captures the quest/objective state
machine in its simplest form. **After the servers shut down this can never be produced again by
anyone.** Prioritize it over polish.

#### SC-AUTH-02 — Cold login, established account `P0`
**Do.** Client fully closed, no cached session. Start envelope → launch client → log in → wait in
hangar 30 s → stamp `END`. Repeat **three times** identically. Then repeat on accounts at
different progression levels (new / mid / maxed / with DLC) recording `account.state` accurately.

**Look for.** The full authentication chain: TLS handshake and SNI, credential submission, token
issuance, token presentation to the lobby server, then the **full-state sync** — the large
server→client burst immediately after auth containing the entire account model (inventory, ships,
modules, currencies, progression, contracts). This burst is the highest-value single sequence in
the archive. Three identical runs separate the static protocol from per-session nonces; different
account states reveal which parts of the blob are variable-length arrays.

Also: the **protocol version / build hash** in the client's first packet. The emulator must
reproduce it exactly or the server rejects it — and it is usually in the first 32 bytes.

#### SC-AUTH-03 — Warm login (cached session / auto-login)
**Do.** Immediately after SC-AUTH-02, close and reopen the client without re-entering
credentials. Full envelope.

**Look for.** The token-resume path, which is shorter and often skips TLS entirely. Reveals token
lifetime, refresh mechanics, and what the client persists locally (check the install directory and
`%LOCALAPPDATA%`, §6.1).

#### SC-AUTH-04 — Failed authentication (all variants) `P0`
**Do.** On a throwaway account, trigger each failure, one per capture, marker naming the expected
failure: wrong password; non-existent username; correct credentials while already logged in
elsewhere; a deliberately corrupted local session file.

**Look for.** The **error code table**. Every distinct failure yields a distinct server response
code — the emulator must return the right one or the client hangs on a spinner instead of showing
a message. Failure paths are what emulator projects universally lack and are cheap to capture.

#### SC-AUTH-05 — Clean logout and client exit
**Do.** Use the in-game logout/exit option. Separately, capture a hard kill
(`Stop-Process -Name StarConflict -Force`). Both need a full envelope.

**Look for.** The graceful teardown sequence vs its absence; whether the server is told, and the
FIN/RST pattern the emulator must tolerate.

---

### 3.2 Hangar, Inventory & Loadout (TCP focused)

> **Method for this entire section: differential capture (§0.2).** Perform each action a minimum
> of three times. A single instance of an action tells a developer almost nothing.

#### SC-HGR-01 — Ship swap
**Do.** With at least four ships owned: stamp → select ship slot 1 → wait 5 s → stamp → slot 2 →
wait 5 s → stamp → back to slot 1 → wait 5 s → stamp. Repeat the cycle three times. Record ship
names/IDs in `notes.md` in the exact order selected.

**Look for.** The "active ship" opcode, and whether ship identity is an index, a database ID, or
a GUID. Returning to slot 1 is the control: an identical byte sequence on both visits confirms a
stable ID rather than a session-local handle.

#### SC-HGR-02 — Module fit / unfit
**Do.** Stamp → remove one module from one slot → wait 3 s → stamp → refit the *same* module to
the *same* slot → wait 3 s → stamp. Then a *different* module in the *same* slot; then the *same*
module in a *different* slot. Three repetitions of each.

**Look for.** Slot indexing, and item **instance** ID vs item **type** ID — the
same-module/different-slot and different-module/same-slot pairs isolate these independently.
Also whether the client sends a full loadout or an incremental delta.

#### SC-HGR-03 — Inventory operations
**Do.** One per capture, three repetitions each, with markers: move an item between tabs;
sort/filter; open an item detail popup; mark favourite/locked; dismantle a low-value item
(record before/after counts).

**Look for.** Which operations are client-local (no packets — an equally valuable finding, note it
explicitly) versus server-authoritative. Inventory paging and lazy-loading on large inventories.

#### SC-HGR-04 — Ship upgrade / synergy / progression spend
**Do.** Stamp → apply a synergy or free-XP conversion → stamp. Unlock a module tier. Record every
relevant counter before and after. Three repetitions.

**Look for.** Progression currency encoding, unlock-tree node identifiers, and the response
confirming a persistent progression write.

#### SC-HGR-05 — Ellydium / crafting-tree progression *(if present in your build)*
**Do.** Any crafting, evolution, or resource-conversion step. Capture inputs and outputs exactly
in `economy_ledger`.

**Look for.** Recipe identifiers and multi-resource transaction encoding. These systems have the
most complex economy payloads in the game and are frequently the least documented.

---

### 3.3 Economy & Store (TCP focused)

#### SC-ECON-01 — Purchase with soft currency `P0`
**Do.** Record credits before. Stamp `before purchase credits=<exact>` → buy one specific item →
stamp `after purchase credits=<exact>`. Repeat **three times with the same item**, then once with
a **different item**, then once with a **different currency**.

**Look for.** The transaction request/response pair; item catalogue IDs; price encoding; and by
diffing against your recorded integers, the **currency field offset and endianness**. Same-item
repetitions isolate the nonce; different-item isolates the item ID; different-currency isolates
the currency type tag.

#### SC-ECON-02 — Sell / refund
**Do.** Sell an item back. Three repetitions. Full ledger entries.

**Look for.** Whether sell is a distinct opcode or a signed-quantity variant of buy.

#### SC-ECON-03 — Purchase failure: insufficient funds `P0`
**Do.** On a low-currency account, attempt to buy something unaffordable. Three repetitions.

**Look for.** The rejection response and its error code. **The emulator must reject invalid
purchases identically or the client desyncs from the server's authoritative balance.**

#### SC-ECON-04 — Other rejection paths `P0`
**Do.** One capture each, where the game permits attempting them: inventory/slot full;
rank-restricted item; faction-locked item; equipping a module to an incompatible ship;
insufficient licence/level.

**Look for.** The full server-side validation table. Each distinct rejection is one more branch
the emulator needs.

#### SC-ECON-05 — Premium currency spend
**Do.** Spend already-owned premium currency (GS) in game. **Do not capture a real-money
purchase** (§0.3) — stop the capture, buy, restart.

**Look for.** Whether hard currency uses a distinct channel or an extra confirmation round-trip.

#### SC-ECON-06 — Player market / trading *(if present in your build)*
**Do.** Browse listings; place a buy order; place a sell order; cancel an order; complete a trade.
One per capture, markers throughout.

**Look for.** Market query pagination, order-book representation, and the asynchronous
fill-notification path — the only common case of an unsolicited server→client economy push.

#### SC-ECON-07 — Contracts, daily missions, rewards
**Do.** Accept a contract; complete it; claim the reward. Separately, log in **across a UTC day
boundary** to capture the daily reset.

**Look for.** Contract state machine, reward grant packets, and server-driven timer/reset logic.

---

### 3.4 Matchmaking, Squads & Session Handshakes

#### SC-MM-01 — Solo queue join and cancel
**Do.** Stamp → join a queue → wait 20 s **in queue without matching** → stamp → cancel → stamp.
Three repetitions. If you match before 20 s, note it and retry in a less populated mode.

**Look for.** Queue-join opcode and mode/parameter encoding; the in-queue polling or push cadence;
the cancel path. Isolating join-and-cancel from an actual match gives the queue protocol without
the match handoff mixed in.

#### SC-MM-02 — Squad formation
**Do.** With a second player, stamp before and after each of: send invite; receive invite; accept;
change squad ship; squad member changes ship; leave squad; disband. Capture from **both clients
simultaneously** if you can (SC-T3-01).

**Look for.** Invite lifecycle and how squad membership is replicated. Symmetric capture shows
exactly what the server tells each participant.

#### SC-MM-03 — Squad queue and synchronized match entry
**Do.** Queue as a squad and enter a match together, markers at each transition.

**Look for.** How the server keeps squad members on the same team and delivers a shared
assignment.

#### SC-MM-04 — Lobby → match server handoff `P0`
**Do.** The most important handshake in the game. Stamp `queue join` → wait → stamp the **instant**
"match found" appears → stamp on accept → stamp the instant the loading screen appears → stamp
the instant you first see your ship in space. Three repetitions minimum, ideally across different
game modes and regions. If you normally capture through a tap or bridge (§1.10), **take at least
one of these on the gaming machine itself** so client-reported addressing is not masked by an
intermediate NAT.

**Look for.**
- The **assignment packet**: the match server's IP and port handed to the client. Look for a
  4-byte address and 2-byte port; determine byte order. This is what the emulator's matchmaker
  must synthesize.
- The **session ticket** proving to the match server that you were legitimately assigned — the
  link between the TCP lobby identity and the UDP match identity.
- The **match configuration blob**: map ID, mode ID, team assignment, and the full player roster
  with ship loadouts. This roster packet is the densest in matchmaking and defines the
  entity-spawn format used for the rest of the match.
- Whether the lobby TCP connection **stays open** during the match:
  ```bash
  tshark -n -r capture_00001*.pcapng -Y "tcp && ip.addr==<LOBBY_IP> && tcp.len>0" \
         -T fields -e frame.time_utc -e tcp.len
  ```
  If it does, you have two concurrent authoritative channels and §3.6's cross-channel correlation
  applies.

#### SC-MM-05 — Match exit and return to lobby
**Do.** From the post-match score screen, return to hangar. Capture through to a stable hangar.

**Look for.** The reverse handoff, match-result submission, and post-match reward grants (which
currencies changed, and on which channel).

#### SC-MM-06 — Custom / private match *(if available)*
**Do.** Create a custom match, configure it, invite a friend, start it.

**Look for.** Room creation and configuration — a rich, fully parameterized description of a
match, and the easiest controlled environment for every combat scenario in §3.5. **If custom
matches are available to you, run all of §3.5 inside one.**

---

### 3.5 Real-Time Combat & Physics (UDP focused)

> **Run these in Practice, PvE, or a private custom match.** Live PvP is uncontrolled: other
> players move, shoot and die inside your capture window, and every extra entity adds noise that
> makes single-axis isolation impossible. Isolation is worth more than realism here.
>
> **Prerequisite: capture SC-BASE-02 first, on the same map, in the same session.**
>
> **Do not run the ETW trace (§2.8) during this section** — the tracing overhead distorts exactly the
> timing you are trying to measure.

#### SC-CBT-01 — Single-axis rotation isolation `P0`
**Do.** Stationary, throttle zero, in open space away from all objects. For each axis separately,
with markers before and after:
1. `pitch up`: hold for exactly 5 s (count with the beacon heartbeat), release, wait 5 s.
2. `pitch down`, then `yaw left`, `yaw right`, `roll left`, `roll right` — same pattern.
3. Between each, return to neutral attitude and wait 5 s.

Touch **only one axis at a time**. Do not fire, boost, or strafe. Use keyboard bindings rather
than mouse — a mouse generates continuous small values on two axes at once and ruins the
isolation.

**Look for.** Which bytes in the client→server input packet change per axis. With one axis moving,
a byte-column diff against the SC-BASE-02 null packet localizes the field immediately. On the
server→client side: the orientation representation. Watch for a **32-bit quantized quaternion**
(the "smallest three" encoding: three 10-bit components plus a 2-bit largest-component index) —
the most common scheme in space sims, recognizable as a fixed 4-byte field that changes smoothly
during rotation and is never all-zero.

#### SC-CBT-02 — Translation isolation
**Do.** Same setup. Separately, with markers: throttle 0→100% held 5 s; 100%→0; strafe left 5 s;
right; up; down; full stop.

**Look for.** Position and velocity fields and their quantization. Compare the on-screen speed
readout to candidate integer fields to derive scale factors.

#### SC-CBT-03 — Afterburner / overdrive state
**Do.** At a constant heading: stamp → afterburner ON, hold 5 s → stamp → OFF, wait 5 s → stamp.
Ten repetitions. Then toggle rapidly (on/off ×10, ~0.5 s each).

**Look for.** A **boolean state bit** in an input bitfield. Ten clean toggles produce ten clean
transitions; the rapid-toggle run confirms whether the state is edge-triggered (a discrete event
message) or level-sampled (a bit in the periodic input packet) — a distinction the emulator must
reproduce or boost behaves wrongly.

#### SC-CBT-04 — Module and ability activation
**Do.** For **each** module and active ability, one at a time with markers: activate, let it run
its full duration, let it fully cool down, wait 5 s. Three repetitions each. Then activate a
module while it is still on cooldown (a deliberate rejection).

**Look for.** Module activation encoding (slot index vs ability ID), server confirmation, cooldown
state replication, and — from the cooldown rejection — the server-side validation the emulator
must implement to prevent ability spam.

#### SC-CBT-05 — Weapon fire: miss `P0`
**Do.** Aim at **empty space**, nothing in line of fire. Stamp → single shot → wait 3 s → stamp.
Ten repetitions. Then a 3-second continuous burst. Then fire to full overheat.

**Look for.** The fire command and its parameters (aim vector, timestamp, predicted-hit claim).
Firing at nothing is the control case: everything here is *firing*, with no *hitting* mixed in.

#### SC-CBT-06 — Weapon fire: hit registration `P0`
**Do.** Against a **stationary** target (a PvE turret, structure, or a cooperative friend holding
still in a custom match). Record the target's hull/shield before and after each shot. Stamp →
single shot → stamp with the observed damage number. Ten repetitions. Then repeat against a
**moving** target at range.

**Look for.** The **authority model** — the highest-value question in the entire combat protocol.
Compare against SC-CBT-05:
- If a hit produces an **extra client→server** packet that SC-CBT-05 lacks, hit detection is
  client-reported and the server validates it.
- If client→server traffic is byte-identical between hit and miss and only the **server→client**
  stream differs, hit detection is fully server-authoritative.

The emulator's entire combat implementation depends on which is true, and this pair answers it
definitively. Also: on-screen damage values versus payload integers give you the damage field and
its scaling; the moving-target run reveals lag compensation and rewind behaviour.

#### SC-CBT-07 — Damage taken: shield and hull `P0`
**Do.** Let a PvE enemy attack you. Stamp at: first shield damage; shield depleted; first hull
damage; hull critical; destruction. Record exact HP values from the HUD at each marker.

**Look for.** Shield and hull as separate fields, their max/current representation, resistance
math, the shield-regeneration state machine (delay and rate — measurable from packet timing at
idle after damage stops), and the destruction event.

#### SC-CBT-08 — Missile lock and launch
**Do.** Stamp → begin lock → stamp the instant lock acquires → wait 3 s → stamp → fire → stamp on
impact or miss. Also capture a lock deliberately **broken** before firing, and a lock defeated by
countermeasures.

**Look for.** Whether lock is a client-side UI state or a server-tracked entity relationship (the
broken-lock case answers this); the missile as a **server-spawned entity** with its own ID and
update stream; and the guidance update rate.

#### SC-CBT-09 — Death and respawn
**Do.** Get destroyed. Stamp at: destruction; respawn screen; ship selection; respawn complete.
Capture through to flying again.

**Look for.** Entity destruction, the spectator/respawn state, and re-spawn entity creation — a
smaller, cleaner version of the initial match entity spawn.

#### SC-CBT-10 — Objective interaction
**Do.** Whichever applies to the mode: capture a beacon, plant/defuse, hold a zone, damage a
structural objective. Markers at start, progress, and completion.

**Look for.** Objective state replication and scoring — the mode-specific logic layer the emulator
must reimplement per game mode.

---

### 3.6 World Persistence, AI & Open Space

> Open Space (Invasion) exercises systems entirely absent from arena matches: persistent world
> state, sector transitions, NPC AI, loot, and cargo. Under-captured relative to its importance.

#### SC-WLD-01 — Enter Open Space `P0`
**Do.** From hangar, launch into an Open Space sector. Markers at: departure initiated; loading;
first frame in space; fully controllable.

**Look for.** Whether this uses the same lobby→match handoff as SC-MM-04 or a different path; the
initial world-state dump (all entities in the sector); the persistence identity distinguishing an
Open Space session from a match instance.

#### SC-WLD-02 — Sector transition / warp gate `P0`
**Do.** Fly to a jump gate and transition to an adjacent sector. Markers at: gate approach;
activation; loading; arrival. **Three repetitions**, including at least one return to the
originating sector.

**Look for.** Whether transition means a new UDP endpoint (server handoff) or a re-handshake on
the existing one. Check the socket sampler and PCAP for a new 5-tuple at the transition marker —
a load-bearing architectural fact for the emulator. The return trip tests whether sector state
persisted.

#### SC-WLD-03 — Station docking and undocking
**Do.** Dock at a station; interact with station services; undock. Markers at each stage. Three
repetitions.

**Look for.** The docking state machine, whether docking suspends the realtime channel or keeps it
live, and how station services (which are hangar-like) are delivered inside a world instance.

#### SC-WLD-04 — Navigation and persistence
**Do.** Fly a long, deliberate route across a sector at constant velocity for 60 s. Then stop for
60 s. Then repeat with rapid direction changes.

**Look for.** Interest management: does the server send everything in the sector, or only entities
near you? Watch for entity create/destroy messages correlated with your movement — that is the
AoI radius, estimable by correlating the marker timeline with your on-screen position when
entities appear and disappear.

#### SC-WLD-05 — NPC / alien spawn triggers `P0`
**Do.** Locate a known spawn point. Stamp → approach from far outside its range → stamp the
instant NPCs appear on radar → stamp when visually acquired → retreat far away → stamp when they
despawn or disengage. Three repetitions.

**Look for.** The spawn trigger condition (proximity? timer? server-scheduled?) and the NPC entity
creation packet including its template/archetype ID. The retreat phase reveals despawn and leash
logic. Correlate the "appears on radar" marker against packet timing to distinguish "spawned now"
from "existed already, entered your AoI".

#### SC-WLD-06 — NPC combat and AI behaviour
**Do.** Engage a single NPC in isolation. Markers at: aggro; NPC opens fire; NPC evades; NPC
destroyed. Then deliberately flee and stamp when it disengages.

**Look for.** Whether NPC positions are full state updates or compressed like player entities;
whether AI state (aggro/patrol/flee) is transmitted explicitly or must be inferred. Explicit AI
state fields make the emulator's job dramatically easier — establishing their presence or absence
early is high-value.

#### SC-WLD-07 — Loot and cargo `P0`
**Do.** Destroy an NPC and stamp the instant loot appears. Approach and collect it, stamping at
collection. Record inventory before and after. Repeat for: mining/harvesting if present; cargo
container pickup; **loot you deliberately leave behind and return to later**.

**Look for.** Loot as world entities versus inventory grants; the drop-table roll result; and
critically the **cross-channel correlation** — does inventory change over the UDP world channel,
or does an event on the TCP lobby channel handle it? Filter both channels around the marker:
```bash
tshark -n -r capture_00001*.pcapng -Y 'frame.time >= "2026-08-14 20:31:40" && frame.time <= "2026-08-14 20:31:50"' \
       -T fields -e frame.number -e ip.src -e ip.dst -e udp.length -e tcp.len
```

#### SC-WLD-08 — Death in Open Space
**Do.** Get destroyed while carrying cargo. Markers at destruction, loot drop, and respawn.

**Look for.** Loss-on-death rules and how cargo is transferred to a world container — the
persistence path with real consequences and therefore strong server-side validation.

#### SC-WLD-09 — Quests / missions
**Do.** Accept a mission from an NPC or terminal; check progress; complete; turn in; claim the
reward. Markers at each. Also capture **abandoning** a mission, and **failing** one.

**Look for.** The quest state machine and objective-progress messages. Abandon and fail paths are
the ones emulators omit.

#### SC-WLD-10 — Special Operations / raids *(if available)*
**Do.** A full Special Operation run with markers at each phase transition and boss mechanic.

**Look for.** Scripted encounter sequencing — the most complex server-driven logic in the game and
the least likely to be recoverable from the client binary alone.

---

### 3.7 Edge Cases & Failure Modes

> These are what emulator projects always lack and what nobody thinks to capture. They are cheap.
> Please capture them.

#### SC-EDGE-01 — Network loss mid-match `P0`
**Do.** In a **PvE/Practice** match, mid-flight, sever connectivity for 90 s, then restore it.

**Break the path, not the link.** If you disable the adapter or pull the cable, the client's retry
packets are never transmitted and therefore never captured — you lose the exact data you wanted.
The retries are the point.

**Best: break it upstream, where your capture still sees the attempts.** Unplug the *router's*
uplink, or pull the cable at the switch rather than at the machine. Your NIC stays up, the client
keeps retransmitting into a void, and every retry lands in the file.

**Second best: a firewall rule that blocks the game's peers by address.** Windows Firewall filters
outbound traffic above the capture point, so blocked packets never reach the wire and are not
recorded — but the client's *behaviour* is still captured, and connections already established
are torn down realistically:

```powershell
# Administrator. Use the peer addresses from §1.3.
New-NetFirewallRule -DisplayName "SC-EDGE-01" -Direction Outbound `
    -RemoteAddress 203.0.113.10,203.0.113.11 -Action Block
# ...wait 90 s...
Remove-NetFirewallRule -DisplayName "SC-EDGE-01"
```

**With a tap or bridge (§1.10), best of all:** block on the capture machine's forward path. The
gaming machine transmits normally, the capture sits between it and the block, and every retry is
recorded exactly as sent.

Whichever you use, **say which one in `notes.md`.** They produce visibly different files, and a
reader deriving a retry cadence from the wrong one derives a wrong number.

**Look for.** Client retry/backoff cadence — the emulator must tolerate it; the server timeout
value; and whether a reconnect/resume path exists.

#### SC-EDGE-02 — Client hard kill and reconnect
**Do.** Mid-match, kill the client outright — `Stop-Process -Name StarConflict -Force`, or End
Task from Task Manager. Not a clean exit: the point is that the client never gets to say goodbye.
Immediately relaunch, log in, and see whether you rejoin the match in progress. Capture the entire
sequence as one bundle.

**Look for.** The **rejoin-in-progress** path — a distinct and complex handshake that is easy to
forget exists until the emulator has to implement it.

#### SC-EDGE-03 — Idle timeout
**Do.** Log in and leave the client **completely untouched** in the hangar until the server
disconnects you (may take 15–60 min). Capture throughout. Good background task — just do not
touch the machine. Stop the display sleeping so OBS keeps a usable recording, and make sure the
machine itself will not suspend:
```powershell
# Administrator. 0 = never. Restore your usual values afterwards.
powercfg /change monitor-timeout-ac 0
powercfg /change standby-timeout-ac 0
```

**Look for.** The idle timeout value and the disconnect notification. Also produces a very long,
very clean keepalive-only baseline.

#### SC-EDGE-04 — Server-initiated disconnect
**Do.** Opportunistic. If you are kicked, hit maintenance, or catch a scheduled shutdown, submit
the capture. **Keep a rolling capture during announced maintenance windows** — shutdown notices
and the shutdown sequence are high-value and time-limited.

**Look for.** Server-initiated teardown messages and how the client presents them.

#### SC-EDGE-05 — Degraded network conditions `T3`
**Do.** Degrade **your own uplink only**, then run SC-CBT-01 and SC-CBT-06 under each profile.

Windows ships no `netem` equivalent. Three workable routes, best first:

1. **Your router's QoS or rate limiter.** Many consumer routers can throttle one client by MAC
   address. This affects only your machine and needs nothing installed.
2. **[clumsy](https://jagt.github.io/clumsy/)** — a small, well-known WinDivert-based tool that
   applies loss, lag and jitter to matched traffic. Filter it to the game's peers from §1.3 so
   nothing else on the machine is affected.
3. **A tap or bridge (§1.10) whose capture machine shapes the forwarded traffic**, if you already
   have that rig.

Record the exact tool, version and parameters in `notes.md`. A loss figure is meaningless without
knowing what produced it, and these three do not shape identically.

Do this **only in Practice/PvE**, never in PvP where it degrades other players' matches.

**Look for.** The **reliable-UDP layer**. Under loss you will see retransmissions, NAKs, or ACK
bitfields that never appear on a clean link. This is the only practical way to discover the
reliability and ordering mechanism sitting on top of UDP, and it is essential — the emulator
cannot implement a transport it has never observed recovering from loss. Under added latency, look
for client-prediction correction packets (the server snapping your position back).

#### SC-EDGE-06 — Client version mismatch
**Do.** Opportunistic, and **guaranteed available on patch day**: attempt to connect with an
outdated client immediately before updating. Steam makes this easy — set the game to "Only update
when I launch it" and capture before allowing the update.

**Look for.** The version-gate rejection and the exact protocol/build identifier compared. The
emulator must present a version the client accepts; this capture shows both sides of that check.

---

### 3.8 Tier 3 — Synchronized Multi-Client Capture

#### SC-T3-01 — Two clients, one match, two simultaneous captures `P0`
The single most valuable capture type for emulator development.

**Do.**
1. Two Windows machines, two accounts, two capture rigs.
2. **Sync both clocks against the same time source** immediately before starting, and record both
   offsets:
   ```powershell
   # Administrator, on each machine:
   w32tm /config /manualpeerlist:"time.cloudflare.com" /syncfromflags:manual /update
   w32tm /resync
   w32tm /query /status        # record the phase offset from each
   ```
3. Run `sccap mark --console` on **both**. If the machines share a LAN, both beacons broadcast
   onto the same link, so **each capture contains both machines' markers** — a hard,
   clock-independent cross-correlation channel. Give each machine a distinct `--volunteer` id so
   the two marker streams are told apart.
4. Enter the **same match** (custom/private if possible), ideally on opposing teams.
5. Run a scripted sequence, both players stamping their own beacon:
   - A flies a known path while B holds still (markers at each waypoint).
   - B fires at A: markers on each shot from both sides.
   - A activates modules while B observes: markers on both.
   - A is destroyed by B: markers on both.
6. Submit **both bundles together**, cross-referencing each other's `bundle_id` in
   `session.json` → `related_bundles`.

**Look for.** Which fields are server-authoritative and replicated versus client-local. Seeing what
the server tells B about A's position, compared to what A told the server, exposes the
interpolation, prediction and lag-compensation pipeline directly. This cannot be inferred from a
single-client capture at any effort level.

#### SC-T3-02 — Gateway / tap capture
**Do.** The tap arrangement in §1.10 — a managed switch mirror port, a bridging second machine,
or a passive tap. Not on the gaming machine.

**Look for.** True wire bytes with zero NIC offload artifacts and zero capture-induced load on the
client. If you have this capability, prefer it for all combat scenarios: it is the gold-standard
framing reference against which host captures can be validated.

---

## 4. Verification & QA — Run Before Every Submission

```powershell
.\out\sccap.exe verify .\captures\SC_20260814T203015Z__ECON-03__vol-042__EU__000
```

Exit code `0` = submittable, `2` = verification failed, `5` = the bundle declares a schema this
build cannot read.

**An interrupted session passes.** If the tool was killed rather than stopped cleanly, `verify`
reports `VERIFIED (interrupted)` and exits 0. That is correct and deliberate: a session is valid
up to the point it stopped, and abrupt termination is an expected way for a capture to end. Only
*inconsistency* fails — a hash that does not match, a structurally broken segment, a claim in
`session.json` contradicted by what is on disk.

### 4.1 Automated checks

`sccap verify` performs all of these. The right-hand column is what to do when one fails.

| # | Check | Pass criterion | If it fails |
|---|---|---|---|
| 1 | Schema | A MAJOR this build understands | Use a newer `sccap`. Never partially read a bundle whose schema you do not know. |
| 2 | Termination | Clean, or interrupted (both fine) | A bundle claiming a clean close with no `utc_end` is inconsistent — report it, it is a tool bug. |
| 3 | **Integrity** | Every file matches `SHA256SUMS` | Something rewrote a file after capture. Do not submit; the evidence is no longer trustworthy. A missing `SHA256SUMS` is expected for an interrupted session — regenerate with `--write-sums`. |
| 4 | Segments | Every `.pcapng` walks end to end | A torn tail on the *last* segment is expected after a kill. An earlier truncated segment means something else went wrong. |
| 5 | **No drops** | `packets_dropped` is 0 | **Fatal.** Traffic crossed the wire and is missing from your file. Close background load and recapture. This is read from the driver's own counter, recorded during capture — it cannot be recovered from the file afterwards, which is why it is checked live in §1.8. |
| 6 | **Not truncated** | `snaplen` is 0 (full frames) | **Fatal.** You captured headers only. Recapture without `--snaplen`. |
| 7 | No capture filter | `capture_filter` empty | Traffic outside the filter was never recorded and cannot be recovered. |
| 8 | Clock anchors | Monotonic, ≥ 2 anchors | A backwards anchor means the clock stepped mid-session; note it in `notes.md`. |
| 9 | Permissions | Owner-only ACL | Something widened access to the bundle. A warning, not a failure — but a session may contain credentials, so check who was granted access. |
| 10 | Sensitivity | Marked sensitive | A bundle not marked sensitive is a tool bug. Report it. |
| 11 | Index | Records resolve to real frames | A truncated tail is expected after a kill; `sccap index --rebuild` regenerates it. |

### 4.2 Manual checks — five minutes, do not skip

These are the ones no tool can do for you.

- [ ] **Eyeball the payload.** Open a segment in Wireshark, apply
      `ip.addr in {<GAME_IPS>} && (udp.length > 8 || tcp.len > 0)`, click a few packets, look at
      the Bytes pane. You should see varied, structured binary. If every payload looks identical,
      you captured keepalives and missed the real traffic. If it is all printable ASCII, note it —
      that is a significant and welcome finding.
- [ ] **Confirm markers reached the PCAP**, not just `markers.log`:
      ```powershell
      .\out\sccap.exe decode .\captures\SC_* --type SCMARK
      ```
      Zero means Windows Firewall ate the beacon datagrams (§2.5). The log is still intact, but
      you have lost video correlation for this session.
- [ ] **Confirm the video and PCAP overlap.** The first and last SCMARK sequence numbers in the
      PCAP must both be visible somewhere in the video.
- [ ] **Confirm the scenario actually happened.** Watch the video at 4× and confirm you did what
      the scenario specifies, in order, with markers. It is common to discover you skipped a
      repetition.
- [ ] **Secret scan.** Search the segments for your throwaway password; expect to find it. That is
      not a bug — the master-server protocol has no transport encryption, which is exactly why
      §0.3 says throwaway accounts only. Record it in `notes.md`, and change that password when
      you stop using the account for captures.
- [ ] **Coverage went down.** `sccap coverage` — the never-observed count should be lower than
      before this session. If it did not move, you recorded something already thoroughly covered;
      that is fine occasionally but pick an unexplored scenario next time.

### 4.3 Independent cross-check (recommended once per rig)

Everything above is `sccap` checking its own work. Once — when you first set the machine up, and
again if you change adapters — verify it against a tool this project did not write:

```powershell
$tshark = "$env:ProgramFiles\Wireshark\tshark.exe"

& $tshark -n -r .\captures\SC_*\capture_00001*.pcapng -q -z conv,udp    # top talkers
& $tshark -n -r .\captures\SC_*\capture_00001*.pcapng -q -z conv,tcp
& $tshark -n -r .\captures\SC_*\capture_00001*.pcapng -q -z io,stat,1   # activity over time
& $tshark -n -r .\captures\SC_*\capture_00001*.pcapng -q -z io,phs      # protocol breakdown

& "$env:ProgramFiles\Wireshark\capinfos.exe" -A .\captures\SC_*\capture_00001*.pcapng
```

`capinfos` should report a packet-size limit of 65535 or none at all. Anything smaller means
frames were truncated at capture time, which no later step can undo.

> The project's own test suite does this comparison rigorously — running `sccap` and `dumpcap` on
> the same interface simultaneously and asserting every journaled frame appears byte-identically
> in the independent capture. Independence is the point; comparing against ourselves would prove
> nothing.

---

## 5. Submission

### 5.1 Packaging

```powershell
.\out\sccap.exe verify .\captures\SC_* --write-sums      # must exit 0

$bundle = Get-Item .\captures\SC_*
Compress-Archive -Path $bundle -DestinationPath "$($bundle.Name).zip"
Get-FileHash "$($bundle.Name).zip" -Algorithm SHA256
```

Resolve every failure. Warnings are acceptable if explained in `notes.md`. Generate `SHA256SUMS`
last, after every other file is final. Do not build one archive spanning multiple bundles. Submit
via the project's coordinated channel, not a public file host (§5.3).

> **The ACL does not travel with the archive.** A bundle is protected on disk by an owner-only
> DACL, and zipping it produces an ordinary file with ordinary permissions containing credentials
> in the clear. Treat the `.zip` as the sensitive object from the moment you create it, and delete
> it once it has been submitted.

### 5.2 Pooled endpoint inventory

Alongside each submission, append your Phase 0 findings to the shared inventory: every hostname
(DNS + SNI), every resolved IP, every port, tagged by region and role (auth / lobby / match / CDN
/ telemetry). This pooled list becomes the emulator's DNS and route redirect map, and it must
cover every region — a volunteer in SEA sees endpoints an EU volunteer never will.

### 5.3 Handling sensitive bundles

Every gameplay bundle contains a session token and an account identifier. Treat bundles as
**sensitive by default**: submit through the coordinated channel; change that account's password
afterwards and stop using it for captures. `SC-BASE-03` (pre-login) is the only category safe to
share publicly with minimal review. If a bundle contains anything unexpected — plaintext
credentials, payment metadata, another player's personal information beyond normal gameplay —
set `restricted: true` and flag it in `notes.md` so intake handles it separately.

---

## 6. Appendices

### 6.1 Archive the client, not just the traffic

**PCAPs without the matching client build are frequently undecodable.** Protocol version gates,
key schedules, message-ID tables, and struct layouts all live in the binary. Every volunteer
should contribute at least one complete client archive.

`sccap` records the build identity into every session automatically (§2.4), so you do not need to
transcribe version numbers by hand. What you *do* need to do is keep a copy of the executable
those identities point at.

```powershell
$stamp = Get-Date -UFormat %Y%m%d
$dest  = "$HOME\sc-archive"
New-Item -ItemType Directory -Force -Path $dest | Out-Null

# sccap already found the install; use the path it prints rather than assuming C:.
$game = "C:\Program Files (x86)\Steam\steamapps\common\star conflict"

# 1. The client itself: executables, DLLs, engine archives (.vromfs.bin) and configs (.blk).
#    Exclude 'data' only if you are short of space -- it may hold item and ship tables.
Compress-Archive -Path $game -DestinationPath "$dest\sc-client-$stamp.zip"

# 2. Local state: saved config, cached session state, crash logs.
Compress-Archive -Path "$env:LOCALAPPDATA\Star Conflict","$env:APPDATA\Star Conflict" `
                 -DestinationPath "$dest\sc-localstate-$stamp.zip" -ErrorAction SilentlyContinue

# 3. Exact version identifiers, from every source that has one.
@(
  "== in-game version string =="
  "<paste from the client UI>"
  "== steam manifest =="
  (Get-Content "C:\Program Files (x86)\Steam\steamapps\appmanifest_212070.acf")
  "== what sccap detected =="
  (& .\out\sccap.exe doctor | Select-String 'game client')
  "== install listing, largest first =="
  (Get-ChildItem $game -File | Sort-Object Length -Descending |
     Select-Object -First 50 | ForEach-Object { "$($_.Length) $($_.Name)" })
) | Out-File "$dest\client_version.txt"

Get-FileHash "$dest\*.zip" -Algorithm SHA256 | Format-List |
    Out-File -Append "$dest\client_version.txt"
```

- [ ] **Every patch from now until shutdown.** Copy the install directory before each update and
      keep both. Set Steam to **"Only update this game when I launch it"** so updates never
      surprise you. Patch deltas near shutdown are especially valuable.
- [ ] **Strip credentials from the local-state archive** before submitting, or flag it
      `restricted`. Cached session tokens live there.
- [ ] **Keep the executable even if you keep nothing else.** It is the only way to recover a
      message's structure if that message was never recorded.

### 6.2 Post-shutdown capture — do not stop when the servers do

After shutdown the client still tries to connect, and that trace is directly actionable.

**Step 1 — capture the client failing normally.** Run a full capture envelope while launching the
client and attempting to log in against dead servers. This alone is worth archiving.

**Step 2 — redirect the endpoints to a local sink and capture what the client says.** Use the
hostname inventory you built in §5.2.

```powershell
# Administrator. Point every game hostname at this machine.
$hosts = "$env:SystemRoot\System32\drivers\etc\hosts"
Copy-Item $hosts "$hosts.backup-before-sc"          # so you can put it back exactly
Add-Content $hosts @"
127.0.0.1  login.example-gaijin-host.net
127.0.0.1  lobby.example-gaijin-host.net
"@
ipconfig /flushdns
```

**Capturing loopback needs the Npcap loopback adapter.** Traffic to `127.0.0.1` never touches a
physical NIC. Npcap installs a loopback capture adapter (usually shown as
`Adapter for loopback traffic capture`) when the "Support loopback traffic" option is selected at
install time — if `sccap doctor` does not list it, re-run the Npcap installer and enable it.

```powershell
.\out\sccap.exe capture --scenario POST-01 --interface "Loopback" --out .\captures
```

A minimal sink that accepts anything and records the first bytes the client sends:

```powershell
# In a separate Administrator terminal, per port from your inventory.
$listener = [System.Net.Sockets.TcpListener]::new([System.Net.IPAddress]::Any, 443)
$listener.Start()
while ($true) {
    $client = $listener.AcceptTcpClient()
    $buf = New-Object byte[] 4096
    $n = $client.GetStream().Read($buf, 0, 4096)
    ($buf[0..($n-1)] | ForEach-Object { $_.ToString("x2") }) -join ' ' |
        Out-File -Append .\tcp-first-bytes.log
    $client.Close()
}
```

Restore the hosts file afterwards:

```powershell
Move-Item "$hosts.backup-before-sc" $hosts -Force
ipconfig /flushdns
```

If the client refuses to talk to a sink presenting no valid certificate, that is itself the
finding — record it, and fall back to reading the plaintext pre-TLS bytes from the loopback
capture. The master-server protocol has no TLS at all, so its first bytes come through regardless.

**Look for.** The **exact first bytes the client sends unprompted** — precisely the first thing
the emulator must parse and answer, isolated from all other traffic. Then, iteratively: what does
the client do when the emulator responds with X? This closes the loop from passive archaeology to
active development, and it is the natural continuation of this project's work.

### 6.3 Quick reference card

```
SETUP (once)      install Npcap + Go (+ Wireshark)
                  cd sc-capture; go build -tags npcap -o out\sccap.exe .\cmd\sccap

BEFORE EACH       open PowerShell AS ADMINISTRATOR
                  .\out\sccap.exe doctor                        # every line OK
                  .\out\sccap.exe doctor --watch 30s            # game running, in hangar
                  Get-NetAdapterLso -Name "Ethernet"            # and Rsc; disable if on

START             OBS -> sccap capture -> 10s idle -> BEGIN marker
                  .\out\sccap.exe capture --scenario CBT-01 --region EU --out .\captures
                  .\out\sccap.exe mark --console                # second admin terminal

DURING            marker before AND after every atomic action; 3+ repetitions each
                  record exact HUD numbers (currency / HP / speed) at every marker
                  WATCH drops=0 -- if it climbs, stop and start over

STOP              END marker -> 10s idle -> Ctrl+C in the capture terminal -> stop OBS

AFTER             .\out\sccap.exe verify .\captures\SC_*        # must exit 0
                  .\out\sccap.exe coverage                      # never-observed must drop
                  complete session.json, write notes.md
                  Enable-NetAdapterLso / -Rsc if you turned them off

NEVER  truncate the snaplen · edit a .pcapng before submitting · guess at ports
       close the console window instead of Ctrl+C · capture on a guessed interface
       delete a capture because something went wrong -- mark it ANOMALY and submit
```

### 6.4 Glossary

| Term | Meaning |
|---|---|
| **Bundle** | A directory containing one scenario's PCAP plus all metadata. The unit of submission. |
| **Envelope** | The standard start/idle/marker/action/marker/idle/stop procedure around every capture (§2.3). |
| **SCMARK** | The UDP marker beacon providing PCAP↔video↔semantic-event binding (§2.5). |
| **Tap / mirror capture** | Recording between the gaming machine and the router rather than on it, for offload-free frames and zero capture load (§1.10). |
| **Npcap** | The Windows packet driver `sccap` records through. A separate install; its licence forbids redistribution, so it cannot be bundled (§1.1). |
| **LSO / RSC** | Large Send Offload and Receive Segment Coalescing — adapter features that glue packets together before Windows sees them, making captured frame boundaries fiction (§1.2). |
| **Offline build** | `sccap` built without `-tags npcap`: reads, verifies and decodes archives with no prerequisites, but cannot record. |
| **Differential capture** | Repeating an action with one variable changed, to localize fields by diffing (§0.2). |
| **Known-plaintext seeding** | Injecting distinctive ASCII into game state as a byte-stream anchor (§2.7). |
| **Full-state sync** | The large post-auth server→client burst carrying the whole account model. |
| **Cross-channel correlation** | Matching events between the TCP control channel and the UDP realtime channel. |
| **AoI** | Area of Interest — the server's per-client entity relevance filter. |
