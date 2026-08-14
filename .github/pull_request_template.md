## What this changes

<!-- One or two sentences. -->

## Effect on the archive

**Required by the constitution (Development Workflow).** Every change must declare this. If a box
is ticked, explain below it.

- [ ] Changes **what is captured** — traffic that would previously have been recorded is not, or
      vice versa
- [ ] Changes **what is persisted** — the contents or durability of a session on disk
- [ ] Changes **the on-disk schema** — if so, the schema version is bumped and readers targeting
      prior versions still work, or refuse explicitly
- [ ] None of the above — this change cannot affect any archived session

Explanation:

## Principle checks

Reviews must verify these three (constitution, Compliance):

- [ ] No capture-time filtering decision introduced (Principle I)
- [ ] No path where a decoder can lose bytes — nothing in `internal/decode` is imported by
      `internal/journal` (Principle II)
- [ ] No in-path behaviour that cannot be disabled (Principle IV)

## Evidence

<!-- Test output, a capture that demonstrates the behaviour, or why neither applies. -->
