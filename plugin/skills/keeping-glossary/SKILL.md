---
name: keeping-glossary
description: Use when a Trellis project needs a glossary or its glossary must change - the user says what a word means or that one word replaces another, something new needs a name the glossary lacks, a term is retired, or the user asks for a glossary. Commands are in trellis:trellis.
---

# Keeping the glossary

A glossary is a Trellis entry built from the `glossary` template: one row per
concept under `## Terms` — **Term**, **Means**, **Not**.
`trellis:using-glossary` reads it; this skill writes it.

## Starting one

1. Call `vault_list` and look for an entry whose `template` is `glossary`. A
   project keeps one, so if it exists, change it instead.
2. Take rows only from words the user or the project already defines. Invent no
   meanings.
3. **If any row needed a choice** — which of two words is the Term, or what a
   word means — show the proposed rows and ask before creating the entry.
4. Create it, then publish it so the project sees it:

       vault_write  {"action": "new", "template": "glossary", "title": "Glossary", "body": "..."}
       vault_share  {"action": "publish", "slug": "glossary"}

5. Ask whether to pin it. Pin only on a yes, with a one-line recap:
   `vault_share` `{"action": "pin", "slug": "<entry>", "recap": "..."}`.

## The table

    ## Terms

    | Term | Means | Not |
    |---|---|---|
    | **bed** | A strip of ground inside a plot. | row, strip |

Each row covers one concept:

- **Means** says what the thing is, in one sentence.
- **Not** lists the words people reach for instead.

Keep the `## Terms` heading; the `glossary` template rejects a body without
it, on every Trellis write.

## Changing it

Read the entry, change one row, then write the whole body back with the
version you read:

    vault_show   {"slug": "<entry>"}
    vault_write  {"action": "edit", "slug": "<entry>", "if_version": <version>, "body": "..."}

| Situation | Do |
|---|---|
| The user says what a word means, or that one word replaces another | Record it now. |
| You need a word the glossary lacks | Propose the row and ask first. |
| A Term is replaced | Change the Term, and move the old word into **Not**. |
| A concept is gone | Delete its row. |

If the entry is pinned, keep its recap to one line. Keep the glossary in this
entry only — not in CLAUDE.md, AGENTS.md or harness memory.
