# Star Conflict Network Archival Protocol

**Version 2.0 — Ubuntu / Linux Volunteer Execution Manual**

This is an operational manual, not a tutorial. Follow it literally. Every rule here exists
because violating it produces a capture that cannot be used for protocol reconstruction after
the servers are gone.

**Platform: Linux only. Ubuntu 22.04 LTS or newer** (24.04 LTS recommended; tested on 24.04 and
26.04). No Windows tooling is used anywhere in this protocol. If a step appears to require
Windows, it is a bug in this document — report it.

**Canonical source:** this file. Tooling lives in [`tools/`](../tools/). Submit corrections as
PRs against this document.

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

### 0.4 Choose your rig profile — do this first

Star Conflict is a Windows title. On Linux it runs through Proton/Wine, and that may or may not
work for your account, your GPU, and the current client build. **Establish which profile you are
on before planning any capture work.**

| | **Profile A — Native Linux host** | **Profile B — Linux capture gateway** |
|---|---|---|
| Game runs on | Ubuntu, via Steam Play / Proton or Wine | Any machine (including a Windows box) |
| Capture runs on | The same Ubuntu host | A separate Ubuntu box routing that machine's traffic |
| Requires | The client to work under Proton | Two NICs (or a managed switch with a mirror port) |
| Gives you | netns isolation, TLS keys via GnuTLS, syscall correlation | Zero offload artifacts, zero capture load on the client, true wire bytes |
| Use when | Proton runs the client acceptably | Proton fails, anti-cheat blocks it, or you want gold-standard wire fidelity |

**Both profiles are fully Linux-side.** Profile B satisfies a Linux-only tooling requirement even
when the game itself will not run on Linux — every capture, verification, and analysis step
happens on Ubuntu.

**They are complementary, not alternatives.** Proton does not alter payload bytes — the game
writes bytes to a socket and the Linux kernel puts those exact bytes on the wire, and UDP
datagram boundaries are preserved exactly — so Profile A is trustworthy for the entire
application-protocol corpus (§3.2–§3.6). What Proton *does* change sits below the payload: the
TLS handshake is GnuTLS rather than SChannel (§1.9), and TCP options, segmentation and keepalive
defaults are Linux's rather than Windows'. So:

| Question you are answering | Trust |
|---|---|
| What do the game's messages look like? Opcodes, entity encoding, physics fields, economy | **Either profile.** Payload bytes are identical. |
| What does the server send, and at what tick rate? | **Either profile.** That is the server talking. |
| What handshake must the emulator accept from a Windows client? | **Profile B only.** |
| What are the transport-level timeouts and keepalive behaviours? | **Profile B**, or annotate that you measured Linux's stack. |
| Can I decrypt the auth flow at all? | **Profile A** is far easier (§1.9). |

If you have the hardware for both, the highest-value split is: **Profile A for TLS key
extraction and bulk scenario volume, Profile B for the fingerprint-accurate reference captures
of §3.1 authentication and §3.7 timeouts.**

**Profile A viability check — run this before anything else:**

```bash
# 1. Install Steam and enable Steam Play for all titles:
#    Steam -> Settings -> Compatibility -> "Enable Steam Play for all other titles"
# 2. Install and launch Star Conflict. Confirm you can reach the hangar and enter a match.
# 3. Confirm which Proton build was used:
ls -dt ~/.steam/steam/steamapps/compatdata/*/ | head
protontricks -l 2>/dev/null || echo "install protontricks for a friendlier listing"
```

If the client will not launch, will not authenticate, or is blocked by anti-cheat, **stop and
switch to Profile B (§1.9).** Do not burn volunteer time fighting Proton — a Profile B gateway
produces better captures anyway.

### 0.5 Capture tiers

Contribute at whatever tier you can sustain. A large volume of correct Tier 1 is worth more than
a handful of botched Tier 3 attempts.

| Tier | Requirements | Adds |
|---|---|---|
| **T1** | Wired Ethernet, quiesced host, `dumpcap` + OBS + marker beacon, session sidecar | The baseline everyone can do |
| **T2** | + network-namespace isolation, TLS key extraction, socket sampler, offloads disabled | Perfectly isolated traffic, decryptable auth, exact endpoint inventory |
| **T3** | + Two synchronized clients in one match, or a Profile B gateway/tap, or loss/latency profiles | Authoritative-vs-local field discrimination, reliability-layer discovery |

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
| 7 | **TLS key material for the auth/launcher flow** | §1.8 | Under Wine this is unusually achievable — see the GnuTLS route. |
| 8 | **Disconnect / reconnect / timeout / kick** | SC-EDGE-* | The paths every emulator gets wrong, and that nobody thinks to capture. |
| 9 | **Two-client synchronized match capture** | SC-T3-01 | The only way to distinguish server-authoritative fields from client-local ones. |
| 10 | **Client binaries, Proton prefix, and version manifest** | §6.1 | PCAPs without the matching client build are frequently undecodable. |

---

## 1. Environment Configuration (Ubuntu)

### 1.1 One-shot bootstrap

```bash
git clone <this-repo> && cd star-conflict-clone
./tools/setup-ubuntu.sh
```

This installs the toolchain, fixes the `dumpcap` permission trap, and verifies clock discipline.
Re-run `./tools/setup-ubuntu.sh --check` before **every** session — it is fast and catches the
regressions that silently ruin captures.

What it installs:

```bash
sudo apt install wireshark-common tshark chrony ethtool iproute2 python3 obs-studio ffmpeg jq zstd
```

**The `dumpcap` permission trap — the #1 Ubuntu setup failure.** Ubuntu ships
`/usr/bin/dumpcap` as `root:wireshark` mode `0754`. Capabilities alone are not enough: a user
outside the `wireshark` group cannot even *execute* the binary, and the error is a bare
`Permission denied` that looks like a capture problem rather than a group problem.

```bash
sudo dpkg-reconfigure wireshark-common      # answer YES to "allow non-superusers to capture"
sudo usermod -aG wireshark "$USER"
# log out and back in -- or, for the current shell only:
newgrp wireshark
getcap /usr/bin/dumpcap                      # expect: cap_net_admin,cap_net_raw=eip
dumpcap -D                                   # must list interfaces without sudo
```

**Never run `dumpcap` under `sudo`.** It drops privileges deliberately; running it as root
writes root-owned capture files into your bundle and breaks the rest of the toolchain.

### 1.2 Host preparation

**Physical layer**
- [ ] **Use wired Ethernet.** Wi-Fi introduces retransmission artifacts and driver-level
      reordering that corrupt timing analysis. If you must use Wi-Fi, record it honestly in the
      sidecar so devs discount your inter-arrival timings.
- [ ] Disable any VPN, WireGuard tunnel, or "gaming accelerator". They re-encapsulate traffic and
      destroy the endpoint inventory. Check with `ip route show` and `ip -br link`.
- [ ] Leave IPv6 enabled and capture it. Accidentally filtering out an IPv6 channel is a silent,
      total data loss.

**NIC offload — this one matters and is usually skipped**

TSO, GSO, GRO and LRO cause the capture to record *coalesced* super-frames that never existed on
the wire. For protocol reversing this destroys the true framing and MSS boundaries.

```bash
./tools/setup-ubuntu.sh --offloads-off eno1      # before capturing
./tools/setup-ubuntu.sh --offloads-on  eno1      # restore afterwards

# equivalently, by hand:
sudo ethtool -K eno1 tso off gso off gro off lro off
ethtool -k eno1 | grep -E '^(tcp-segmentation|generic-segmentation|generic-receive|large-receive)-offload'
```

Some NICs report `[fixed]` for an offload — it cannot be changed and that is harmless. Record in
the sidecar which offloads you actually disabled. If you could not disable them, say so — devs
will treat your frame boundaries as untrustworthy rather than deriving a wrong MSS.

**Clock discipline**

Timestamps are how PCAPs from different volunteers get correlated. An unsynced clock makes a
Tier 3 dual-client capture worthless.

```bash
sudo apt install chrony
sudo systemctl enable --now chrony
sudo chronyc makestep                  # step the clock immediately
chronyc tracking                       # record 'Reference ID' and 'System time' offset
timedatectl show -p NTPSynchronized --value   # must print: yes
```

Record the measured offset in milliseconds in `session.json` → `clock.offset_ms_at_start`.
`start-capture.sh` reads it from `chronyc` automatically.

**Quiesce the host**

The correct capture filter is *no capture filter* (§1.5), and network-namespace isolation (§1.4)
makes quiescing largely unnecessary. Without isolation, before every session close: web
browsers, Discord, Nextcloud/Dropbox/rclone, `unattended-upgrades`, Flatpak/snap auto-refresh,
backup timers.

```bash
sudo systemctl stop unattended-upgrades.service
systemctl --user stop nextcloud-client.service 2>/dev/null
sudo snap refresh --hold=24h 2>/dev/null
# see what is actually talking:
sudo ss -tunp state established
```

> **Do NOT close Steam, and do NOT filter out Steam traffic.** If the client authenticates via a
> Steam session ticket, that exchange is part of the auth flow the emulator must service. Steam
> traffic is in scope.

### 1.3 Phase 0 — Endpoint discovery

You cannot write a correct filter for an application whose endpoints you have not measured. Do
not copy port numbers from forum posts; derive them.

1. **Find the process tree.** Under Proton the game is not one process — Steam spawns `reaper`,
   which spawns `pressure-vessel`, which spawns `wine`, which spawns the `.exe`. Any of them may
   own the socket.

   ```bash
   pgrep -a -f -i 'conflict|wine|proton|pressure-vessel'
   ps -ef --forest | grep -A20 -i steam | head -40
   ```

2. **Run the socket sampler**, then play through login → hangar → matchmaking → a match → back
   to hangar:

   ```bash
   ./tools/watch_game_sockets.py --out sockets.csv --filter-file display-filter.txt
   # or target the whole Steam tree explicitly:
   ./tools/watch_game_sockets.py --pid "$(pgrep -f '^/.*/steam$' | head -1)"
   # Ctrl+C when done
   ```

   It samples `/proc` every 250 ms, follows the entire descendant tree (re-resolved each tick, so
   the game process is picked up when it launches), records every distinct socket, and writes a
   ready-to-paste Wireshark display filter.

   *It deliberately over-collects.* If a pattern matches Steam itself, every Steam child is
   included. That is safe — an inclusive display filter that is slightly too broad costs nothing,
   while a too-narrow one is fatal. Use `--pid` when you want precision.

3. **Recover UDP peers from the capture.** UDP is connectionless, so the sampler can only see the
   *local* port. Match it against the PCAP to find the remote match-server endpoints:

   ```bash
   tshark -n -r capture_00001_*.pcapng -Y "udp.port == <LOCAL_UDP_PORT>" \
          -T fields -e ip.src -e ip.dst -e udp.srcport -e udp.dstport | sort -u
   ```

4. **Commit the result to the pooled endpoint inventory** (§5.2). Every volunteer's endpoints go
   into one list, which becomes the emulator's DNS/route redirect map.

**Expected shape (a hypothesis to verify, not a fact to assume):** a TLS/HTTPS auth and
launcher/CDN phase on TCP 443, a persistent TCP control channel to a lobby/master server, and a
separate UDP flow to a match server whose address is handed to the client at matchmaking time.
Your Phase 0 output either confirms this or — more valuably — contradicts it. Report
contradictions.

### 1.4 Network-namespace isolation (Tier 2 — strongly recommended)

**This is the Linux capability with no Windows equivalent, and it removes the single largest
source of error in this protocol: guessing a filter.**

Run the game in a dedicated network namespace connected to the host by a veth pair. Capture on
the host side of the veth and the file contains the game's traffic and *nothing else* — no
browser, no Discord, no OS telemetry, no cloud sync. There is no filter to get wrong, and nothing
unexpected can be silently excluded.

```bash
sudo ./tools/netns-capture.sh up          # namespace + veth + NAT, offloads off on the veth
sudo ./tools/netns-capture.sh status      # verify DNS and ICMP inside the namespace

# Steam is single-instance: FULLY QUIT any running Steam first, or the game launches
# outside the namespace and outside your capture.
sudo ./tools/netns-capture.sh run steam

# capture on the veth, in another terminal
./tools/start-capture.sh -s CBT-01 -v vol-042 -r EU -i sc-host

sudo ./tools/netns-capture.sh down        # tear down, restore sysctls, leave no residue
```

> **`status` needs `sudo`.** Probing inside a namespace requires root. Run without it and the
> connectivity lines report **UNKNOWN**, not failure — the namespace is very likely fine. Do not
> tear down a working rig on the strength of an unprivileged `status`.

`down` removes the namespace, veth, NAT rules and per-namespace `resolv.conf`, and restores
`net.ipv4.ip_forward` to whatever it was before `up` — it does not leave forwarding enabled
behind you.

Set `host.netns_isolated: true` and `host.interface: "sc-host"` in the sidecar.

**Verify isolation before trusting a session.** Capture for ten seconds with the game idle; if
you see anything that is not the game, isolation is not working. Confirm the game is really
inside:

```bash
ip netns pids sccap        # should list the wine/game processes
```

**Three caveats that change what you capture, so read them:**
- **IPv6.** `up` configures a ULA subnet with NAT66 **only if the host has a default IPv6
  route**. Many home connections do not, so `ipv6 : DISABLED` is common and expected — but it
  means the namespace is IPv4-only and an IPv6 game channel would be silently absent, the exact
  failure §1.5 tells you never to risk. `sudo ... status` reports which case you are in. Check
  it, and if IPv6 is disabled, rely on your non-isolated captures to rule out an IPv6 channel.
- Inside the namespace the client sits behind NAT at `10.77.0.2`, so its observed local address
  differs from a normal run.
- Anything the protocol does with client-reported addresses (NAT punch-through, peer hints) will
  look different. For SC-MM-04 handoff behaviour specifically, prefer a non-isolated capture.

Because of all three, **take at least one non-isolated capture per scenario family** so the true
topology and address family are preserved somewhere in the archive.

Further caveats (X11 abstract sockets, Steam single-instance) are documented at the bottom of
`tools/netns-capture.sh`.

### 1.5 Capture filters (BPF, applied at capture time)

> **Default policy: capture with NO capture filter.**
> A capture filter is irreversible. Anything it drops is gone forever, and the traffic most
> likely to be dropped by a naive filter is exactly the traffic nobody predicted — a NAT
> punch-through to an unexpected subnet, a telemetry channel, a CDN fetch, a fallback relay.
> With netns isolation there is nothing to filter anyway. Take the disk hit.

Use a filter only if you are disk- or CPU-constrained (`start-capture.sh --low-disk`). It removes
only LAN service chatter that is definitively not the game, and **explicitly preserves the marker
beacon**:

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

Paste the sampler's `display-filter.txt`, which looks like:

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

# Exclude LAN noise if you captured unfiltered without netns isolation
!(mdns || llmnr || nbns || ssdp || dhcp || igmp)
```

Filtering by IP is strictly better than filtering by application name. An **inclusive** filter
built from Phase 0 endpoints is exact; name-based exclusion of browsers and chat clients is not.

### 1.7 Wireshark profile settings

Create a dedicated profile (**Edit → Configuration Profiles → +**, name it `star-conflict`), then
share `~/.config/wireshark/profiles/star-conflict/` with your first submission so others can drop
it straight in.

| Setting | Value | Why |
|---|---|---|
| Protocols → TCP → *Validate the TCP checksum if possible* | **Off** | Checksum offload marks valid packets as bad and hides them from filters. |
| Protocols → TCP → *Allow subdissector to reassemble TCP streams* | **On** | Needed to see application PDUs spanning segments. |
| Protocols → TCP → *Reassemble out-of-order segments* | **On** | |
| Protocols → UDP → *Try heuristic sub-dissectors first* | **Off** | Stops Wireshark mis-dissecting the game protocol as RTP and hiding the raw bytes. |
| View → Name Resolution → *Resolve network addresses* | **Off** | Prevents Wireshark's own DNS lookups polluting a live capture. |
| Time Display Format | **UTC date and time of day**, precision **Milliseconds** | Correlates with markers and video. Never use "Seconds Since Beginning of Capture" for archival work. |

Custom columns (Preferences → Columns): `frame.time_utc`, `frame.time_delta_displayed`,
`udp.length`/`tcp.len`, `udp.stream`/`tcp.stream`.

### 1.8 Capture invocation

**Use `dumpcap`, never the Wireshark GUI, for real captures.** The GUI dissects live, which costs
CPU and causes kernel-level packet drops under load — exactly during combat, exactly when you
need the data.

```bash
./tools/start-capture.sh -s ECON-03 -v vol-042 -r EU
```

That wrapper creates the correctly-named bundle, writes a pre-filled `session.json`, runs
pre-flight checks (wireless link, offloads, NTP), and invokes:

```bash
dumpcap -i eno1 -n -s 262144 -B 512 \
        -w "<bundle>/capture.pcapng" -b duration:600 -b filesize:200000
```

| Flag | Meaning | Why it is set this way |
|---|---|---|
| `-s 262144` | Snapshot length | **Never truncate.** The payload is the entire point. A default-truncated capture is unusable and this is the single most common fatal mistake. |
| `-n` | No name resolution | Avoids self-generated DNS in your own capture and saves CPU. |
| `-B 512` | 512 MB kernel buffer | Prevents drops during combat bursts. Raise to 1024 if drops are reported. |
| `-b duration:600` | Roll file every 600 s | Keeps files navigable. |
| `-b filesize:200000` | Roll every ~200 MB (value in kB) | Keeps files under practical upload limits. |
| *(no `-b files:N`)* | — | **Critical.** Adding `-b files:N` makes it a *ring buffer* that deletes your oldest data. Omit it so files accumulate. |

Check for drops afterwards — `start-capture.sh` tees dumpcap's summary to `dumpcap.log`:

```bash
grep -iE 'packets (captured|dropped)' <bundle>/dumpcap.log
```

### 1.9 TLS / SSL session key extraction

The goal is an **NSS key log** that lets Wireshark decrypt the auth and launcher traffic. Work
through this in order.

**Linux has a real advantage here, with one real cost.** Under Wine/Proton the client's Windows
SChannel calls are serviced by Wine's `secur32`, which on most builds is backed by **GnuTLS** —
and GnuTLS honours `SSLKEYLOGFILE`. The Windows-native case (where SChannel ignores it entirely
and you are pushed to a MITM proxy) frequently does not apply here.

> **The cost: a Proton capture does not record a Windows client's TLS handshake.**
> The ClientHello is generated by GnuTLS, not SChannel, so its cipher-suite ordering, extension
> set and overall fingerprint differ from what a native Windows client sends. Everything *inside*
> the session is unaffected — but the handshake itself is Wine's, not the game's.
>
> This matters for two things, and only these two: reproducing a client the live server would
> have accepted if it ever fingerprinted TLS, and knowing what the emulator must accept from the
> Windows clients most users will run. **Capture at least one auth-flow handshake from a native
> Windows client via a Profile B gateway (§1.10)** as the fingerprint reference, even if all your
> decrypted auth work comes from Proton. Record `client.runtime` accurately in every bundle so
> analysts know which they are looking at.

#### Step 1 — Set `SSLKEYLOGFILE` via Steam launch options

No environment-propagation dance is needed. Right-click the game → **Properties → Launch
Options**:

```
SSLKEYLOGFILE=/home/YOURUSER/sc-archive/sslkeys.log %command%
```

For a non-Steam / plain Wine launch:

```bash
mkdir -p ~/sc-archive
SSLKEYLOGFILE=~/sc-archive/sslkeys.log wine /path/to/game.exe
```

Add `PROTON_LOG=1` to the launch options as well — it writes `~/steam-<appid>.log`, which
records the Proton build and any Wine errors, and belongs in the bundle.

#### Step 2 — Verify keys are actually being written

```bash
wc -l ~/sc-archive/sslkeys.log
head -2 ~/sc-archive/sslkeys.log
```

A working keylog contains lines beginning with `CLIENT_RANDOM` (TLS 1.2) or
`CLIENT_HANDSHAKE_TRAFFIC_SECRET` / `CLIENT_TRAFFIC_SECRET_0` / `SERVER_TRAFFIC_SECRET_0`
(TLS 1.3). **An empty or absent file means this route does not work for this client — do not
assume it silently succeeded.** Record the result either way in `session.json` → `tls`.

#### Step 3 — Identify the TLS stack (only if Step 2 failed)

```bash
GAMEPID=$(pgrep -f -i conflict | head -1)
sudo lsof -p "$GAMEPID" | grep -Ei 'gnutls|openssl|libssl|libcrypto|nss'
grep -Ei 'gnutls|libssl|libcrypto|nss' "/proc/$GAMEPID/maps" | awk '{print $6}' | sort -u
```

| Libraries loaded | Implication |
|---|---|
| `libgnutls.so*` | Wine's schannel backend. `SSLKEYLOGFILE` should work — re-check Step 1. |
| `libssl.so*` / `libcrypto.so*` | OpenSSL. `SSLKEYLOGFILE` works only if built with the keylog callback; go to Step 4. |
| `libnss3.so` | NSS. `SSLKEYLOGFILE` works. |
| None, but TLS on the wire | Statically linked or custom crypto. Go to Step 4. |

Record the finding in `tls.stack_detected`. This is itself a useful data point even when
extraction fails.

#### Step 4 — eBPF interception with `ecapture` (no CA, no proxy)

`ecapture` attaches uprobes to the TLS library and dumps plaintext without touching the
certificate chain, so **certificate pinning does not defeat it**. This is the cleanest fallback
on Linux and has no Windows equivalent.

```bash
# https://github.com/gojue/ecapture -- download the release binary for your kernel
sudo ./ecapture tls --help                 # check flag names for YOUR version before relying on them
sudo ./ecapture tls --pcapfile=./decrypted.pcapng   # auto-detects OpenSSL/GnuTLS/NSS
sudo ./ecapture tls -m keylog -k ./sslkeys.log      # or emit a keylog instead
```

If auto-detection misses the library (likely under Wine, where it is loaded via `secur32`),
point `ecapture` at it explicitly — the flag for this has changed across releases, so take the
name from `--help` rather than from here. Find the library first:

```bash
grep -Eo '/[^ ]*libgnutls[^ ]*' "/proc/$(pgrep -f -i conflict | head -1)/maps" | sort -u
```

Requires a kernel with BTF (Ubuntu 22.04+ stock kernels have it):

```bash
ls /sys/kernel/btf/vmlinux && echo "BTF present"
```

#### Step 5 — Intercepting proxy (last resort)

Only if Steps 1–4 all fail. [PolarProxy](https://www.netresec.com/?page=PolarProxy) writes the
*decrypted* traffic straight to a PCAP, which is exactly the archival artifact we want:

```bash
./PolarProxy -p 443,80 -x rootCA.cer -o ./decrypted_pcaps/
```

Caveats to record in the sidecar: certificate pinning defeats this (if the client errors during
login with the proxy active, it pins — set `tls.pinning_suspected: true` and fall back to
capturing ciphertext). A proxy also changes the wire-level endpoints, so **always take a matching
un-proxied capture of the same scenario** so the true topology is preserved.

#### Step 6 — Bind the keys to the capture, permanently

Keys are useless if separated from their PCAP, and volunteer bundles get shuffled. Inject the
secrets **into** the pcapng as a Decryption Secrets Block so they can never be lost:

```bash
editcap --inject-secrets tls,sslkeys.log capture_00001.pcapng capture_00001_keyed.pcapng
```

Verify by opening the `_keyed` file with no key preference configured — if application data
appears decrypted, it worked. Ship **both** the keyed file and the raw `sslkeys.log`, and set
`tls.injected_into_pcapng: true`.

#### Step 7 — Even if all of this fails, capture anyway

Encrypted bytes plus the client binary are still a solvable problem — the key schedule lives in
the executable and can be reversed later. An *uncaptured* handshake is unsolvable forever. **Never
skip a scenario because you could not decrypt it.**

Two things to extract regardless of decryption, because both feed the emulator's redirect map:

```bash
# Every hostname the client asked for (SNI)
tshark -n -r capture_00001.pcapng -T fields -e tls.handshake.extensions_server_name | sort -u | grep .

# Every hostname resolved, with answers
tshark -n -r capture_00001.pcapng -Y "dns.flags.response==1" \
       -T fields -e dns.qry.name -e dns.a | sort -u
```

### 1.10 Profile B — the Ubuntu capture gateway

Use when Proton will not run the client, or when you want the best possible wire fidelity. The
gaming machine plugs into the Ubuntu box; the Ubuntu box routes to the internet and captures in
the middle. **No capture software runs on the gaming machine at all**, so there is zero
capture-induced load and zero offload distortion.

```bash
# eno1 = uplink to the router, enp3s0 = cable to the gaming machine
sudo sysctl -w net.ipv4.ip_forward=1
sudo ip addr add 10.88.0.1/24 dev enp3s0 && sudo ip link set enp3s0 up
sudo nft -f - <<'NFT'
table inet scgw {
  chain postrouting { type nat hook postrouting priority srcnat;
    ip saddr 10.88.0.0/24 oifname "eno1" masquerade }
  chain forward { type filter hook forward priority filter;
    iifname "enp3s0" oifname "eno1" accept
    iifname "eno1" oifname "enp3s0" ct state related,established accept }
}
NFT
sudo apt install dnsmasq
sudo dnsmasq -i enp3s0 --dhcp-range=10.88.0.50,10.88.0.100,12h --no-daemon &

# offloads OFF on the capture interface, then capture -- this is the only interface that matters
./tools/setup-ubuntu.sh --offloads-off enp3s0
./tools/start-capture.sh -s CBT-01 -v vol-042 -r EU -i enp3s0
```

Set `host.profile: "B"` in the sidecar. A managed switch with a mirror/SPAN port, or a passive
network tap feeding a second NIC, works identically and is preferable if you have one — capture
on the mirrored interface with `ip link set <iface> up` and no IP assigned.

For Profile B, run the marker beacon **on the gaming machine** so its packets traverse the
gateway and land in the capture. If the gaming machine cannot run Python, run the beacon on the
gateway instead and record `video.marker_overlay: false` — you lose frame-accurate video sync and
fall back to clock correlation, so say so in `notes.md`.

---

## 2. The Modular Capture Protocol

### 2.1 The bundle is the unit of submission

Never submit a bare `.pcapng`. The unit of work is a **bundle directory** containing the capture
plus everything needed to interpret it.

```
SC_20260814T203015Z__AUTH-02__vol-042__EU__000/
├── capture_00001_20260814203015.pcapng   # dumpcap output (may be several)
├── capture_00002_20260814204015.pcapng
├── session.json                          # REQUIRED -- the sidecar, see §2.4
├── markers.log                           # REQUIRED -- SCMARK beacon log
├── dumpcap.log                           # REQUIRED -- proves the captured/dropped counts
├── sockets.csv                           # T2 -- socket sampler output
├── display-filter.txt                    # T2 -- generated Wireshark filter
├── sslkeys.log                           # T2 -- if TLS extraction succeeded
├── steam-<appid>.log                      # T2 -- PROTON_LOG output
├── video.mkv                             # REQUIRED for gameplay scenarios
├── client_version.txt                    # REQUIRED -- see §6.1
├── notes.md                              # anything surprising that happened
└── SHA256SUMS                            # REQUIRED -- generated last
```

A bundle missing `session.json` or `markers.log` will be rejected at intake. This is not
bureaucracy: without them the capture cannot be placed on a timeline and cannot be correlated
with any other volunteer's data.

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

No spaces, no colons, no local time, ASCII only. `start-capture.sh` generates this for you —
use it rather than naming by hand.

### 2.3 Segmentation — how to avoid submitting a 4-hour blob

**The rule: one scenario, one bundle.** Start the capture immediately before the scenario, stop
it immediately after. Do not leave `dumpcap` running across scenarios.

Every capture is wrapped in this envelope:

1. **Start** `start-capture.sh`.
2. **Start** the marker beacon if not already running.
3. **10 seconds of deliberate idle.** Touch nothing. This lead-in captures the steady state
   immediately before your action, which is what the diff is taken against.
4. **Stamp a marker** naming the scenario: type `BEGIN AUTH-02` + Enter in the marker console.
5. **Perform the scenario**, stamping a marker before *and* after each atomic sub-action.
6. **Stamp** `END AUTH-02`.
7. **10 seconds of deliberate idle** lead-out.
8. **Stop** `dumpcap` (Ctrl+C), then the beacon.

Hard limits:

- **Soft cap 10 minutes / 200 MB per file**, enforced automatically by `-b`.
- **Hard cap 30 minutes per bundle** except a full match or Open Space session, which get their
  own scenario IDs.
- **If something unplanned happens** — do not delete the capture. Stamp `ANOMALY <what
  happened>`, finish the envelope, note it in `notes.md`, and submit it flagged. Unplanned events
  are frequently the most informative captures in the archive.

**Never:** run one capture across login → match → logout; use a ring buffer; stop and restart
`dumpcap` mid-scenario; or edit/re-save a PCAP before submitting. Filtering is the dev team's job
and is destructive when done wrong.

### 2.4 The session sidecar (`session.json`)

Machine-readable metadata, validated at intake against
[`tools/session.schema.json`](../tools/session.schema.json). `start-capture.sh` writes a
pre-filled stub with the Linux/Proton fields already populated; you complete the `<FILL IN>`
placeholders afterwards. `verify_capture.py` fails the bundle if any remain.

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

  "clock": { "ntp_source": "time.cloudflare.com", "offset_ms_at_start": -3.2,
             "method": "chronyc tracking" },

  "client": {
    "game_version": "1.7.x", "build_id": "<from client_version.txt>",
    "platform": "Ubuntu 24.04.1 LTS / 6.8.0-45-generic",
    "runtime": "proton-9.0-3", "launcher": "steam",
    "process_name": "<from Phase 0>", "locale": "en"
  },

  "host": {
    "profile": "A", "link": "wired-1000M", "nic": "Intel I225-V",
    "interface": "sc-host", "netns_isolated": true,
    "offloads_disabled": ["tso", "gso", "gro", "lro"],
    "capture_tool": "Dumpcap (Wireshark) 4.2.2", "capture_filter": "", "snaplen": 262144,
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
  "tls": { "keylog_present": true, "keylog_lines": 214, "injected_into_pcapng": true,
           "stack_detected": "gnutls", "pinning_suspected": false, "proxy_used": "none" },

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

**The solution is a marker that appears in both media simultaneously.** `tools/sc-marker.py`
broadcasts a small ASCII UDP datagram onto the local link once per second:

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

```bash
./tools/sc-marker.py --session SC-ECON-03 --log <bundle>/markers.log
# Heartbeats emit automatically once per second.
# Type a label + Enter at any time to stamp an EVENT marker:
BEGIN ECON-03
before purchase  credits=1450320
after purchase   credits=1201320
END ECON-03
```

Inside a network namespace, run the beacon inside it too so its packets cross the veth:

```bash
sudo ./tools/netns-capture.sh run ./tools/sc-marker.py --session SC-ECON-03 --dest 10.77.0.255
```

Verify markers survived into the capture:

```bash
tshark -n -r <bundle>/capture_00001*.pcapng -Y 'udp contains "SCMARK"' -T fields -e frame.number | wc -l
```

Zero means the beacon was not running, was firewalled, or the capture filter excluded broadcast.
Fix it and recapture — a gameplay bundle without markers has no video correlation.

### 2.6 Screencast configuration

**OBS Studio** (`sudo apt install obs-studio`, or the Flatpak for a newer build).

| Setting | Value | Why |
|---|---|---|
| Container | **MKV** | Survives a crash mid-recording; MP4 does not. Remux afterwards if you like. |
| Resolution | Native, no downscale | HUD numbers must be legible — they are the ground truth for damage, currency, and speed. |
| FPS | **60** | 30 is acceptable but halves your temporal resolution against a 30 Hz snapshot stream. |
| Encoder | **VAAPI** (Intel/AMD) or **NVENC** (NVIDIA) | Software x264 steals CPU from the capture and causes packet drops. |
| Rate control | CQP/CRF ~20 | Quality matters more than file size; a low bitrate smears HUD text. |
| Audio | **On** | Free extra sync channel; weapon and hit audio cues time-align to combat events. |

**Wayland vs X11.** Ubuntu defaults to Wayland, where OBS window/screen capture goes through the
PipeWire portal (`xdg-desktop-portal-gnome`) and requires a per-session permission grant. If
capture is unreliable, log in on **"Ubuntu on Xorg"** from the gear icon at the login screen.
Lightweight Wayland-native alternatives:

```bash
wf-recorder -f video.mkv -c libx264 -p crf=20        # wlroots compositors
gpu-screen-recorder -w screen -f 60 -o video.mkv     # NVIDIA/AMD, very low overhead
```

**Required on-screen elements**, arranged so they never overlap the HUD:

1. The **marker beacon console** (small terminal, bottom-right, always-on-top) so the sequence
   number is readable in every frame.
2. A **UTC clock with milliseconds** — belt and braces if the beacon dies mid-session:
   ```bash
   watch -n0.1 -t 'date -u +%H:%M:%S.%3N'
   ```
3. Any **HUD element the scenario measures**: currency counters for economy, speed/throttle for
   physics, damage numbers for combat.

Start OBS **before** `dumpcap`, stop it **after**. Record `video.start_utc` in the sidecar.

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
they survive `grep` and stay recognisable under simple transforms. Record every seeded string in
`known_plaintext.seeded_strings`.

### 2.8 Bonus Linux annotation channels (Tier 2/3, optional)

Neither replaces a PCAP, but both give a second, independent view that makes ambiguous captures
decidable. Run either alongside a normal capture, never instead of one.

**Wine socket tracing** — logs every winsock call the client makes, with arguments, which maps
API-level intent onto wire bytes:

```bash
WINEDEBUG=+winsock,+wsock32 %command%      # in Steam launch options
# writes to the Proton log; keep it with the bundle
```

**Syscall correlation** — timestamps every `sendto`/`recvfrom` the game performs:

```bash
sudo strace -f -tt -T -e trace=network -p "$(pgrep -f -i conflict | head -1)" -o syscalls.log
```

`strace` slows the process noticeably. Never use it during a physics or hit-registration
scenario where timing fidelity matters; it is for hangar and economy work.

---

## 3. Step-by-Step Scenario Checklist

**Format.** Every scenario states **Do** (exact player actions), **Capture** (envelope and
required artifacts), and **Look for** (what the developer will extract — included so you
understand why precision matters, and so you can tell when a capture went wrong).

**Universal preconditions for every scenario below:**
- [ ] `./tools/setup-ubuntu.sh --check` passes
- [ ] Offloads disabled on the capture interface, clock NTP-synced
- [ ] Capture started via `start-capture.sh` (enforces `-s 262144`, no ring buffer)
- [ ] Marker beacon running, console visible in the screencast
- [ ] OBS (or `wf-recorder`) recording, HUD elements visible
- [ ] 10 s idle lead-in and lead-out
- [ ] Sidecar completed afterwards; `verify_capture.py` passes

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
lifetime, refresh mechanics, and what the client persists locally (check the Proton prefix, §6.1).

#### SC-AUTH-04 — Failed authentication (all variants) `P0`
**Do.** On a throwaway account, trigger each failure, one per capture, marker naming the expected
failure: wrong password; non-existent username; correct credentials while already logged in
elsewhere; a deliberately corrupted local session file.

**Look for.** The **error code table**. Every distinct failure yields a distinct server response
code — the emulator must return the right one or the client hangs on a spinner instead of showing
a message. Failure paths are what emulator projects universally lack and are cheap to capture.

#### SC-AUTH-05 — Clean logout and client exit
**Do.** Use the in-game logout/exit option. Separately, capture a hard kill
(`pkill -9 -f conflict`). Both need a full envelope.

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
game modes and regions. **Prefer a non-netns capture here** (§1.4) so client-reported addressing
is not masked by NAT.

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
> **Do not run `strace` (§2.8) during this section** — the syscall overhead distorts exactly the
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

**Use the drop-after-the-capture-point method, not an unplugged cable.** If you take the link
down, the client's retry packets are never transmitted and therefore never captured — you lose
the exact data you wanted. Dropping *after* the veth keeps every retry visible:

```bash
sudo ./tools/netns-capture.sh block      # sever connectivity, retries still cross sc-host
# ...wait 90 s...
sudo ./tools/netns-capture.sh unblock    # restore
```

For a Profile B gateway, the same idea on the gateway's own forward chain:

```bash
sudo nft add rule inet scgw forward iifname "enp3s0" drop
sudo nft flush chain inet scgw forward   # then re-apply the accept rules from §1.10
```

Plain Tier 1 with no isolation cannot do this cleanly — an `OUTPUT` chain DROP happens *before*
the AF_PACKET tap, so the retries are invisible. Take the link down instead
(`sudo ip link set eno1 down`), accept that you capture the recovery rather than the retries, and
note the limitation in `notes.md`.

**Look for.** Client retry/backoff cadence — the emulator must tolerate it; the server timeout
value; and whether a reconnect/resume path exists.

#### SC-EDGE-02 — Client hard kill and reconnect
**Do.** Mid-match, `pkill -9 -f conflict`. Immediately relaunch, log in, and see whether you
rejoin the match in progress. Capture the entire sequence as one bundle.

**Look for.** The **rejoin-in-progress** path — a distinct and complex handshake that is easy to
forget exists until the emulator has to implement it.

#### SC-EDGE-03 — Idle timeout
**Do.** Log in and leave the client **completely untouched** in the hangar until the server
disconnects you (may take 15–60 min). Capture throughout. Good background task — just do not
touch the machine. Disable the screen blanker so OBS keeps a usable recording:
```bash
gsettings set org.gnome.desktop.session idle-delay 0
```

**Look for.** The idle timeout value and the disconnect notification. Also produces a very long,
very clean keepalive-only baseline.

#### SC-EDGE-04 — Server-initiated disconnect
**Do.** Opportunistic. If you are kicked, hit maintenance, or catch a scheduled shutdown, submit
the capture. **Keep a rolling capture during announced maintenance windows** — shutdown notices
and the shutdown sequence are high-value and time-limited.

**Look for.** Server-initiated teardown messages and how the client presents them.

#### SC-EDGE-05 — Degraded network conditions `T3`
**Do.** Using `tc netem` on **your own uplink only**, run SC-CBT-01 and SC-CBT-06 under each
profile. In a netns setup, apply it to the veth so only the game is affected:

```bash
sudo tc qdisc add dev sc-host root netem loss 5%
sudo tc qdisc change dev sc-host root netem delay 150ms
sudo tc qdisc change dev sc-host root netem delay 50ms 20ms distribution normal
sudo tc qdisc del dev sc-host root                  # remove
```

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
1. Two Ubuntu machines (or one machine plus a second in a separate netns), two accounts, two
   capture rigs.
2. **Sync both clocks against the same NTP source** immediately before starting, and record both
   offsets:
   ```bash
   sudo chronyc -a 'burst 4/4' && sudo chronyc makestep && chronyc tracking
   ```
3. Run the marker beacon on **both**. If the machines share a LAN, both beacons broadcast onto the
   same link, so **each capture contains both machines' markers** — a hard, clock-independent
   cross-correlation channel. Use distinct `--session` labels.
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
**Do.** Profile B (§1.10): capture at the Ubuntu gateway, a managed switch mirror port, or a
passive tap — not on the gaming machine.

**Look for.** True wire bytes with zero NIC offload artifacts and zero capture-induced load on the
client. If you have this capability, prefer it for all combat scenarios: it is the gold-standard
framing reference against which host captures can be validated.

---

## 4. Verification & QA — Run Before Every Submission

```bash
./tools/verify_capture.py SC_20260814T203015Z__ECON-03__vol-042__EU__000/
./tools/verify_capture.py <bundle>/ --secret 'my-throwaway-password' --write-sums
```

Exit code `0` = submittable, `1` = blocked, `2` = environment/usage problem.

### 4.1 Automated checks

| # | Check | Pass criterion | If it fails |
|---|---|---|---|
| 1 | Files open | `capinfos` exits 0 on every capture | File truncated — `dumpcap` was killed. Recapture. |
| 2 | **Not truncated** | `Packet size limit` ≥ 65535 | **Fatal.** You captured headers only. Recapture with `-s 262144`. |
| 3 | Non-empty | Packet count > 100 | Wrong interface. Verify with `dumpcap -D`. |
| 4 | **No drops** | `dumpcap.log` reports 0 dropped | The kernel buffer overflowed and the file has holes. Raise `-B` to 1024, close background load, recapture. `capinfos` cannot see this — drops never reach the file — which is why `dumpcap.log` is required. |
| 5 | Duration sane | Matches scenario, ≤ 30 min (except AUTH-01/WLD/MM-04/EDGE-03/T3) | Split by scenario and recapture. |
| 6 | **Game traffic present** | ≥ 1 *remote* peer with > 50 KB payload | You captured only noise. The capturing host is auto-detected (it appears in ≥80% of frames) and excluded, so this measures real peers. |
| 7 | **Markers present** | ≥ 2 SCMARK frames bracketing the capture | Beacon not running or broadcast filtered. **Recapture.** |
| 8 | Payload non-trivial | Many frames with `udp.length > 8` or `tcp.len > 0` | You captured only ACKs and keepalives. |
| 9 | Required files present and non-empty | `session.json`, `markers.log`, `dumpcap.log`, `client_version.txt` | Fix before submitting. |
| 10 | Sidecar valid | Validates, `bundle_id` matches the directory, **no placeholder of any shape** left | Complete the sidecar — `<FILL IN>` *and* option stubs like `<steam\|lutris\|standalone>` are both rejected. |
| 11 | Non-public peers | Warns if every busy peer is private | Expected for a gateway/netns capture; otherwise a VPN is re-encapsulating. |

### 4.2 Manual checks — five minutes, do not skip

- [ ] **Eyeball the payload.** Open in Wireshark, apply
      `ip.addr in {<GAME_IPS>} && (udp.length > 8 || tcp.len > 0)`, click a few packets, look at
      the Bytes pane. You should see varied, structured binary. If every payload looks identical,
      you captured keepalives and missed the real traffic. If it is all printable ASCII, note it —
      that is a significant and welcome finding.
- [ ] **Confirm the video and PCAP overlap.** The first and last SCMARK sequence numbers in the
      PCAP must both be visible somewhere in the video.
- [ ] **Confirm the scenario actually happened.** Watch the video at 4× and confirm you did what
      the scenario specifies, in order, with markers. It is common to discover you skipped a
      repetition.
- [ ] **Confirm netns isolation actually held** (if used): `ip netns pids sccap` listed the game,
      and the capture contains no non-game traffic.
- [ ] **Secret scan.** `verify_capture.py --secret` covers this; expect zero hits. Hits are a
      genuine protocol finding — record in `notes.md`, flag the bundle `restricted`, change that
      password.
- [ ] **Generate checksums last**, after every other file is final (`--write-sums`, or
      `cd <bundle> && sha256sum * > SHA256SUMS`).

### 4.3 Quick manual equivalents

```bash
capinfos -A capture_00001*.pcapng                       # everything at a glance
tshark -n -r capture_00001*.pcapng -q -z conv,udp       # top talkers
tshark -n -r capture_00001*.pcapng -q -z conv,tcp
tshark -n -r capture_00001*.pcapng -Y 'udp contains "SCMARK"' \
       -T fields -e frame.time_utc -e data.data | head   # marker sanity
tshark -n -r capture_00001*.pcapng -q -z io,stat,1      # activity over time
tshark -n -r capture_00001*.pcapng -q -z io,phs         # protocol breakdown
```

---

## 5. Submission

### 5.1 Packaging

```bash
./tools/verify_capture.py <bundle>/ --write-sums          # must exit 0
tar -I 'zstd -10' -cf <bundle>.tar.zst <bundle>/
sha256sum <bundle>.tar.zst
```

Resolve every FAIL. WARNs are acceptable if explained in `notes.md`. Generate `SHA256SUMS` last.
Do not use a solid archive spanning multiple bundles. Submit via the project's coordinated
channel, not a public file host (§5.3).

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

```bash
APPID=$(ls ~/.steam/steam/steamapps/compatdata/ | head)   # confirm against the store page URL
GAMEDIR=~/.steam/steam/steamapps/common/"Star Conflict"

# 1. the game install, including engine archives (.vromfs.bin) and configs (.blk)
tar -I 'zstd -8' -cf sc-client-$(date -u +%Y%m%d).tar.zst -C "$(dirname "$GAMEDIR")" "$(basename "$GAMEDIR")"

# 2. the Proton prefix -- registry, saved config, cached session state
tar -I 'zstd -8' -cf sc-prefix-$(date -u +%Y%m%d).tar.zst \
    -C ~/.steam/steam/steamapps/compatdata "$APPID"

# 3. exact version identifiers, all three sources
{ echo "== in-game version string =="; echo "<paste from the client UI>"
  echo "== steam manifest =="; cat ~/.steam/steam/steamapps/appmanifest_${APPID}.acf
  echo "== proton build =="; cat ~/steam-${APPID}.log 2>/dev/null | head -20
  echo "== install listing =="; find "$GAMEDIR" -maxdepth 2 -printf '%s %p\n' | sort -rn | head -50
} > client_version.txt
sha256sum sc-client-*.tar.zst sc-prefix-*.tar.zst >> client_version.txt
```

- [ ] **Every patch from now until shutdown.** Copy the install directory before each update and
      keep both. Set Steam to "Only update this game when I launch it" so updates never surprise
      you. Patch deltas near shutdown are especially valuable.
- [ ] **Strip credentials from the prefix archive** before submitting, or flag it `restricted`.

### 6.2 Post-shutdown capture — do not stop when the servers do

After shutdown the client still tries to connect, and that trace is directly actionable.

**Step 1 — capture the client failing normally.** Run a full capture envelope while launching the
client and attempting to log in against dead servers. This alone is worth archiving.

**Step 2 — redirect the endpoints to a local sink and capture what the client says.** Use the
hostname inventory you built in §5.2.

```bash
# point every game hostname at this machine (one line per host from the inventory)
sudo tee -a /etc/hosts <<'EOF'
127.0.0.1  login.example-gaijin-host.net
127.0.0.1  lobby.example-gaijin-host.net
EOF

# capture loopback -- note: -i lo, not your NIC, since the traffic never leaves the box
dumpcap -i lo -n -s 262144 -w postmortem.pcapng &

# minimal sinks that accept anything and hex-dump the first bytes.
# ports below 1024 need root; run these in separate terminals.
sudo socat -x -v TCP-LISTEN:443,reuseaddr,fork /dev/null 2>tcp-first-bytes.log
     socat -x -v UDP-RECVFROM:31337,fork      /dev/null 2>udp-first-bytes.log
```

Remove the `/etc/hosts` lines afterwards. If the client refuses to talk to a sink presenting no
valid certificate, that is itself the finding — record it, and fall back to reading the plaintext
pre-TLS bytes from the loopback capture.

**Look for.** The **exact first bytes the client sends unprompted** — precisely the first thing
the emulator must parse and answer, isolated from all other traffic. Then, iteratively: what does
the client do when the emulator responds with X? This closes the loop from passive archaeology to
active development, and it is the natural continuation of this project's work.

### 6.3 Quick reference card

```
SETUP (once)      ./tools/setup-ubuntu.sh
BEFORE EACH       ./tools/setup-ubuntu.sh --check
                  ./tools/setup-ubuntu.sh --offloads-off eno1
ISOLATE (T2)      sudo ./tools/netns-capture.sh up
                  sudo ./tools/netns-capture.sh status          # sudo, and check the ipv6 line
                  sudo ./tools/netns-capture.sh run steam       # quit Steam first!
START             OBS -> start-capture.sh -> sc-marker.py -> 10s idle -> BEGIN marker
DURING            marker before AND after every atomic action; 3+ repetitions each
                  record exact HUD numbers (currency / HP / speed) at every marker
STOP              END marker -> 10s idle -> Ctrl+C dumpcap -> beacon -> OBS
AFTER             session.json -> verify_capture.py --write-sums -> submit
                  sudo ./tools/netns-capture.sh down
                  ./tools/setup-ubuntu.sh --offloads-on eno1

dumpcap -i <if> -n -s 262144 -B 512 -w <bundle>/capture.pcapng -b duration:600 -b filesize:200000
                        ^^^^^^^^^ never truncate     ^^^^ no `-b files:` = no ring buffer

NEVER  run dumpcap under sudo · truncate snaplen · use a ring buffer · guess at ports
       run a 4-hour blob · edit a PCAP before submitting · capture real-money purchases
       inject traffic at live servers · strace during physics scenarios
ALWAYS UTC · markers · 3 repetitions · exact before/after numbers
       submit anomalies rather than deleting them
```

### 6.4 Glossary

| Term | Meaning |
|---|---|
| **Bundle** | A directory containing one scenario's PCAP plus all metadata. The unit of submission. |
| **Envelope** | The standard start/idle/marker/action/marker/idle/stop procedure around every capture (§2.3). |
| **SCMARK** | The UDP marker beacon providing PCAP↔video↔semantic-event binding (§2.5). |
| **Profile A / B** | Game-on-Ubuntu-via-Proton vs Ubuntu-gateway-capturing-another-machine (§0.4). |
| **netns isolation** | Running the game in a dedicated network namespace so the capture contains only its traffic (§1.4). |
| **Differential capture** | Repeating an action with one variable changed, to localize fields by diffing (§0.2). |
| **Known-plaintext seeding** | Injecting distinctive ASCII into game state as a byte-stream anchor (§2.7). |
| **Full-state sync** | The large post-auth server→client burst carrying the whole account model. |
| **Cross-channel correlation** | Matching events between the TCP control channel and the UDP realtime channel. |
| **AoI** | Area of Interest — the server's per-client entity relevance filter. |
