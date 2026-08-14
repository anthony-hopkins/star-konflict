# Protocol reference data — provenance

**Imported**: 2026-08-13 | **Status**: frozen snapshot, not a live dependency

This directory holds the known protocol element universe and the reference material it was
derived from. It exists so that `sc-capture` can bootstrap its never-observed set (Principle III,
FR-020–024) without depending on any repository outside this workspace.

## Source

| | |
|---|---|
| Origin | `https://github.com/sc-re/sc-proxy` |
| Commit | `968f1a3f1127a6b1ef3da010cadab895dac4dd33` (2026-06-25) |
| Licence | **Unlicense** — public domain, no attribution or copyleft obligation |
| Imported by | One-time `git clone --depth 1`; the repository is no longer part of this workspace |

The Unlicense is why this import is friction-free: the tables are data released into the public
domain and may be copied, modified and redistributed under any licence `sc-capture` later
adopts. (The Go server reimplementation, `sc-re/star-conflict-revitalized`, is EUPL-1.2 and
copyleft — nothing from it is imported here, and that is deliberate.)

Ultimately these names were not authored by anyone in the project: they were extracted from
string tables in the shipped game client (see `source/AC_ptrs`, a Ghidra listing of the pointer
table at VMA `0x08fe75e0`, and the message-type table at VMA `0x08fe7ac0`). They are observations
about a binary, re-derivable from the client by anyone with a disassembler.

## Normalised element universe

The three files `sc-capture` consumes. Together they are the 404 known protocol elements against
which coverage is measured.

| File | Elements | Shape |
|---|---:|---|
| `message-types.json` | 39 | `{id, name}` — `SCMD_*` / `CCMD_*` / `CSCMD_*` by wire type index 0–38 |
| `async-requests.json` | 249 | `{id, name, handler_vma}` — `AC_*` opcodes carried in `CSCMD_ASYNC_REQ` (type 13) |
| `notifications.json` | 116 | `{id, name}` — `SN_*` types carried in `SCMD_NOTIFICATION` (type 14) |

**These are names, not layouts.** The opcode *names* are almost fully recovered; the *body
layouts* are mostly not — roughly 32 of 249 `AC_*` bodies had decoders at the time of import.
That gap is the point of the coverage feature: an element in these tables that has never appeared
on the wire is a countdown item, and an element observed but not decodable is safe forever.

## Raw sources (`source/`)

Kept verbatim so nothing is lost in normalisation, and so a future maintainer can re-derive the
tables and check this import.

| File | What it is |
|---|---|
| `ac_dispatch_table.json` | `[id, handler_vma, name]` for all 249 `AC_*` opcodes |
| `ac_handler_fields.json` | Per-opcode field information recovered from handlers |
| `ServerNotifications.by_id` | `id = SN_NAME` for all 116 notification types |
| `AC_ptrs` | Ghidra listing of the client's opcode-name pointer table — the ultimate source |
| `client.ksy`, `server.ksy` | Kaitai Struct schemas for observed message bodies |
| `protocol.py` | Reference implementation of TGP framing and the MurmurHash2 checksum |
| `scmd_decoders.py` | Message-type table and body decoders, with an inline coverage table |
| `LICENSE.txt` | The Unlicense, as it applied to all of the above |

`protocol.py` is the most important file here after the tables. It documents the 12-byte
big-endian header, the `0x1337533d` MurmurHash2 seed, and — the detail most likely to be got
wrong on a reimplementation — that the checksum is computed over a **little-endian** rendering of
the header even though the wire format is big-endian.

## Rules for this directory

- **Never executed.** `source/` contains two Python files. They are frozen evidence, not code:
  nothing here is run, imported, built, shipped, or installed, and no part of the project depends
  on a Python interpreter existing. The project is Go only (constitution v2.1.0). `protocol.py` is
  kept because golden test vectors are generated from it **by hand, once**, and checked in — it is
  the only independent thing we can check our checksum implementation against, and deleting it to
  satisfy a language rule would destroy that.
- **Frozen.** Nothing here is re-fetched automatically and nothing depends on GitHub staying up.
- **Reference, not truth.** Per the constitution's development workflow, this material is a
  starting hypothesis to verify against observed traffic, not ground truth. A decoder is still
  not written for a message shape that has not been observed.
- **Additive updates only.** If an element is later found to be missing or misnamed, correct it
  here with a note; do not silently renumber. Coverage state keys off these ids.
