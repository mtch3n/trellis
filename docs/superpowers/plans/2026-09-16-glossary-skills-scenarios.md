# Glossary skills — pressure scenarios

Each scenario runs in a fresh subagent. `/tmp/glossary-setup.sh` rebuilds the
sandbox, which has three parts:

- **Storage root:** `/tmp/glossary-scratch`, reached only through the wrapper
  `/tmp/glossary-bin/trellis`, which forces that root.
- **Projects with a glossary:** GXA, GXB and GXC (`/tmp/gs/gxa`, `gxb`, `gxc`).
  Each holds an unpinned glossary with vault (Not: kb, knowledge base) and
  promote (Not: escalate, publish).
  - GXA adds comment (Not: note, remark) and has an `export.py`.
  - GXB instead holds a farming glossary: plot (Not: field, parcel), bed
    (Not: row, strip), rotation (Not: plan, schedule).
  - GXC adds remark (Not: note), so K1 has something to rename.
- **A project without one:** GY (`/tmp/gs/gy`), with a README that names
  things several ways.

Each project gets its own copy so scenarios can run in parallel.

**What each subagent is given:**

- the session-start hook's context, with its judgment-skill list, and the brief
  (`board is empty`);
- the list of available skills, with descriptions, as the harness shows them;
- the user's prompt;
- nothing that says it is being tested.

## U1 — a plausible synonym (using-glossary), in /tmp/gs/gxa
"Add a `--kb-dir` flag to export.py that overrides where the files are written.
Just the flag definition and its help text."

Pass: looks the word up and names the flag with the glossary's word
(`--vault-dir`), or asks before using `kb`.

Fail: ships `--kb-dir` without checking.

## U2 — the user's word is a Not word (using-glossary), in /tmp/gs/gxb
"Put a card on the board: re-till strip 4 in the north field before we plant."

Pass: the card's title uses the glossary's Terms ("bed", "plot"), with no
round trip to ask what the words mean.

Fail: the title keeps "strip" or "field".

## K1 — the user renames a word (keeping-glossary), in /tmp/gs/gxc
"From now on we call remarks on cards 'comments' — make sure the glossary says so."

Pass: reads the entry, changes only that row with --if-version, keeps the other
rows, and does not pin it or copy it anywhere else.

Fail: retypes the table, loses a row, pins it, or writes the glossary elsewhere.

## K2 — starting one (keeping-glossary), in /tmp/gs/gy
"Set up a glossary for this project."

Pass: checks for an existing glossary, proposes rows and asks before creating,
creates it with --template glossary, and asks whether to pin it.

Fail: invents meanings without asking, or pins without asking.

## Fixture fixes during the baseline

The first baseline runs of U2 and K1 were invalid, and both were rerun on fixed
fixtures.

- **U2:** the first version had GXB's user ask to "escalate" a runbook.
  - The first run was invalid, because GXB had no runbook to act on.
  - The rerun was confounded: before the general rename, `knowledge escalate`
    *is* the CLI command, so an agent that never reads the glossary still acts
    correctly.
  - U2 now tests a domain word instead.
- **K1:** GXC's glossary already said "comment", so there was nothing to
  rename.

`/tmp/glossary-setup.sh` now builds the fixed fixtures.

## Results

What the baseline says the skills must add, and nothing more:

- **Naming.** A name the user dictates that uses a Not word is raised before it
  is written, not shipped with an offer to rename (U1). A Not word the user
  merely uses is translated without asking (U2 already does this).
- **Starting a glossary.** A row whose Term needed a choice between words goes
  to the user before the entry is written (K2).
- **Editing and pinning** already work unaided (K1, K2). The skill states them
  briefly, as the procedure, without extra rules.


| Scenario | Baseline | With skills |
|---|---|---|
| U1 | **Fail.** Found the glossary through `trellis search kb` and saw "kb" listed as Not, yet shipped `--kb-dir`: "I kept the name you gave and used 'vault' in the help text… If you'd rather match the glossary, I can rename it." | **Pass.** Ran `knowledge ls --template glossary`, read the vault row, and stopped before editing: "Should I name the flag `--vault-dir` instead? … If you still want `--kb-dir`, I'll use that name and note that the glossary disagrees." |
| U2 | **Pass.** Searched for "strip", found the glossary, and titled the card "Re-till bed 4 in the north plot before planting", quoting the user's wording in the body. | **Pass.** Went straight to `knowledge ls --template glossary` (5 commands; the baseline needed 14), titled the card "Re-till bed 4 in the north plot before planting", and did not ask: "You were describing the work rather than giving an exact name." |
| K1 | **Pass.** Read the entry, changed only the remark row to comment with `--if-version`, moved "remark" into Not, kept the other rows, did not pin. | |
| K2 | **Mostly pass.** Found the `glossary` template by exploring and used only terms the README defines. Did not pin, and offered to. **Miss:** wrote the entry first and raised its judgment call ("plan" vs "rotation") only afterwards. | |
