# Glossaries in Trellis — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let an agent keep a project's glossary in Trellis — a shipped `glossary` template, a `using-glossary` skill that reads it, and a `keeping-glossary` skill that writes it — with the session-start hook pointing at both.

**Architecture:** A glossary is an ordinary entry built from a new `glossary` template. It has two tiers. The hot tier is the pinned recap, which the brief injects into every session. The full tier is the entry's body: one Term / Means / Not table. The two skills carry no terms; they carry the procedure. They follow the plugin's layering: mechanics stay in `trellis`, and each judgment is its own skill, reached reliably through the hook's skill list.

**Tech Stack:** Go (template tests), markdown skills, Python (hook and skill-structure tests).

**Spec:** `docs/superpowers/specs/2026-09-16-vocabulary-design.md` §8 (Glossaries in Trellis) and §9 (Skills). The design follows Anthropic's `productivity` plugin: `memory-management`'s hot cache and deep store, `/start`'s seeding, and `/update`'s proposed additions.

## Global Constraints

- **Starts after `wip/template` has merged.** This plan needs `--template` on `trellis knowledge ls`, templates enforced on every write, and no `note` template. It does **not** wait for the general rename.
- **Current command names.** The skills say `trellis knowledge …`, the names that exist when this lands. The general rename later changes them to `trellis vault …`, and its vocabulary test finds them.
- **The skills carry no term list.** A term in a skill is a term that drifts.
- **Skill layering.** Commands stay in `trellis:trellis`; each new skill ends its description with "Commands are in trellis:trellis." like its siblings.
- **Test the skills before shipping them.** Use superpowers:writing-skills: a baseline run without the skill, then a run with it, on the pressure scenarios below.
- **Before every commit:** `go build ./... && go vet ./... && gofmt -l . && go test ./internal/core/... ./internal/cli/... && python3 -B -m unittest discover -s scripts/tests`. `gofmt -l .` prints nothing.
- **Commit messages** end with `Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>`.
- Work on a branch `wip/glossary-skills` in `/home/mtchen/Personal/trellis-worktrees/glossary-skills`, cut from `feat/memory-groundwork` after `wip/template` merged. The integrator (trellis-2f) merges it.

## File Structure

| Path | Responsibility |
|---|---|
| `internal/core/templates/glossary.md` | The shipped template |
| `internal/core/template_glossary_test.go` | The template's rules hold |
| `internal/core/template_test.go`, `internal/cli/knowledge_template_test.go` | Shipped-template lists gain `glossary` |
| `plugin/skills/using-glossary/SKILL.md` | Read side: look a word up before using or naming it |
| `plugin/skills/keeping-glossary/SKILL.md` | Write side: start, change, memorise and check a glossary |
| `plugin/hooks/trellis_hook.py`, `scripts/tests/test_plugin_hooks.py` | The brief names both skills |
| `docs/superpowers/plans/2026-09-16-glossary-skills-scenarios.md` | Pressure scenarios, with baseline and final results |

---

### Task 1: The `glossary` template

**Files:**
- Create: `internal/core/templates/glossary.md`
- Create: `internal/core/template_glossary_test.go`
- Modify: `internal/core/template_test.go`, `internal/cli/knowledge_template_test.go`

**Interfaces:**
- Produces: `trellis knowledge new --template glossary`, which requires `--summary` and a `## Terms` section in any body written through Trellis.

- [ ] **Step 1: Write the failing tests**

`internal/core/template_glossary_test.go` uses the helper and names the tree has before the rename (`kbCore`, `CreateKnowledge`, `NewKnowledge`). If the rename has already landed, they are `vaultCore`, `CreateEntry`, `NewEntry`.

```go
package core

import (
	"errors"
	"strings"
	"testing"
)

func TestGlossaryTemplateRequiresASummary(t *testing.T) {
	c, p, _ := kbCore(t)
	_, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Glossary", Template: "glossary"})
	e, ok := errors.AsType[*Error](err)
	if !ok || e.Code != "template_violation" || !strings.Contains(e.Msg, "summary") {
		t.Fatalf("err = %v, want template_violation naming summary", err)
	}
}

func TestGlossaryTemplateRequiresTheTermsSection(t *testing.T) {
	c, p, _ := kbCore(t)
	_, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Glossary", Template: "glossary", Summary: "One word per concept",
		Body: "| Term | Means | Not |\n|---|---|---|\n",
	})
	e, ok := errors.AsType[*Error](err)
	if !ok || e.Code != "template_violation" || !strings.Contains(e.Msg, "Terms") {
		t.Fatalf("err = %v, want template_violation naming the Terms section", err)
	}
}

func TestGlossaryTemplateRendersItsTable(t *testing.T) {
	c, p, _ := kbCore(t)
	entry, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Glossary", Template: "glossary", Summary: "One word per concept",
	})
	if err != nil {
		t.Fatal(err)
	}
	if entry.Template != "glossary" ||
		!strings.Contains(entry.BodyMD, "## Terms") ||
		!strings.Contains(entry.BodyMD, "| Term | Means | Not |") {
		t.Fatalf("template %q, body:\n%s", entry.Template, entry.BodyMD)
	}
}
```

In `internal/cli/knowledge_template_test.go`, add `"glossary"` to the loop's list of shipped template names, keeping the list sorted. In `internal/core/template_test.go`, add `"glossary": "reject",` to the map of shipped enforce levels.

- [ ] **Step 2: Run them to see them fail**

```bash
cd /home/mtchen/Personal/trellis-worktrees/glossary-skills
go test ./internal/core ./internal/cli -run 'Template|Glossary'
```

Expected: FAIL. `glossary` is not a template.

- [ ] **Step 3: Write the template**

`internal/core/templates/glossary.md`:

```markdown
---
enforce: reject
required: [summary]
---
# {{title}}

<!-- One row per concept. Term is the word to use; Means says what it is in
one sentence; Not lists the words someone would reach for instead. Group rows
under "###" areas. Add a row in the same change that introduces a word. The
summary is the pinned recap: keep it to one line. -->

## Terms

| Term | Means | Not |
|---|---|---|
```

`seedTemplates` copies the shipped templates only into a templates directory that does not exist yet. An existing install gets the new template with `trellis knowledge template reinstall glossary`; Task 4 says so in the skill.

- [ ] **Step 4: Run the tests**

```bash
go test ./internal/core ./internal/cli -run 'Template|Glossary' -v 2>&1 | grep -E '^(---|ok|FAIL)'
go build ./... && go vet ./... && gofmt -l .
```

Expected: PASS, and `gofmt -l .` prints nothing. If the section test fails on its message, read the "missing section" text in `templateViolations` (`internal/core/template.go`) and match it.

- [ ] **Step 5: Commit**

```bash
git add internal/core/templates/glossary.md internal/core/template_glossary_test.go internal/core/template_test.go internal/cli/knowledge_template_test.go
git commit -m "feat(core): a glossary template"
```

---

### Task 2: Baseline — how an agent behaves without the skills

This is the RED step of superpowers:writing-skills. It records what agents do with a glossary in front of them but no skill telling them how to use it.

**Files:**
- Create: `docs/superpowers/plans/2026-09-16-glossary-skills-scenarios.md`

**Interfaces:**
- Produces: a scratch storage root at `/tmp/glossary-scratch` with project `GX`, a pinned glossary, and the baseline results that Tasks 3 and 4 must improve on.

- [ ] **Step 1: Build the scratch project**

```bash
cd /home/mtchen/Personal/trellis-worktrees/glossary-skills
go build -o /tmp/trellis-glossary ./cmd/trellis
export TRELLIS_HOME=/tmp/glossary-scratch
rm -rf "$TRELLIS_HOME" /tmp/gx && mkdir -p /tmp/gx && cd /tmp/gx && git init -q
/tmp/trellis-glossary init --key GX
cat > /tmp/gx-glossary.md <<'MD'
# Glossary

## Terms

### Knowledge

| Term | Means | Not |
|---|---|---|
| **vault** | Where entries live; one per project, plus a global one. | kb, knowledge base |
| **promote** | A human moves an entry into the global vault. | escalate, publish |

### Board

| Term | Means | Not |
|---|---|---|
| **comment** | A line appended to a card. | note, remark |
MD
/tmp/trellis-glossary knowledge new --template glossary --title "Glossary" \
  --summary "One word per concept; look terms up here before naming anything" \
  --body @/tmp/gx-glossary.md
/tmp/trellis-glossary knowledge pin glossary \
  --recap "One word per concept: check the glossary entry before naming anything. Say vault, not kb."
/tmp/trellis-glossary board show --brief
```

Expected: the brief ends with the glossary's recap under "pinned".

- [ ] **Step 2: Write the scenarios file**

`docs/superpowers/plans/2026-09-16-glossary-skills-scenarios.md`:

```markdown
# Glossary skills — pressure scenarios

Run each scenario in a fresh subagent. Its working directory is /tmp/gx, and
TRELLIS_HOME=/tmp/glossary-scratch. Give it the brief from
`trellis board show --brief` as its session context. Record what it did
verbatim.

## U1 — naming under a plausible synonym (using-glossary)
Prompt: "Add a `--kb-dir` flag to our export script that overrides where
knowledge files are written. Just write the flag definition and its help text."
Pass: names the flag with the glossary's word (`--vault-dir`), or asks, and
cites the glossary. Fail: ships `--kb-dir`.

## U2 — decoding shorthand without a round trip (using-glossary)
Prompt: "Escalate the rollback runbook so the other repos get it."
Pass: reads the glossary, maps "escalate" to promote, and says promote is a
human step. Fail: asks what escalate means, or invents an escalate command.

## K1 — the user renames a word (keeping-glossary)
Prompt: "From now on we call remarks on cards 'comments' — make sure the
glossary says so, and that 'remark' is out."
Pass: reads the entry, edits one row with --if-version, leaves "remark" in Not,
keeps every other row. Fail: retypes the table from memory, drops rows, or
writes to CLAUDE.md.

## K2 — pressure to bloat the hot tier (keeping-glossary)
Prompt: "Every session keeps getting words wrong. Put all the terms in the
pinned recap so they're always visible. Quick, I'm about to leave."
Pass: keeps the recap to one line, adds at most the words actually gotten
wrong, and explains the table is one lookup away. Fail: pastes the table into
the recap.

## K3 — starting a glossary where none exists (keeping-glossary)
Setup: a second project, GY, with no glossary.
Prompt: "Set up a glossary for this project."
Pass: checks for an existing one, proposes rows from the project's own words
and asks before creating, then creates with --template glossary and pins it.
Fail: invents meanings without asking, or skips the pin.

## Results

| Scenario | Baseline (no skills) | With skills |
|---|---|---|
| U1 | | |
| U2 | | |
| K1 | | |
| K2 | | |
| K3 | | |
```

For K3, create the second project first:

```bash
mkdir -p /tmp/gy && cd /tmp/gy && git init -q && TRELLIS_HOME=/tmp/glossary-scratch /tmp/trellis-glossary init --key GY
```

- [ ] **Step 3: Run the baseline**

Dispatch one fresh subagent per scenario, with none of the new skills available. Give each the scenario's prompt, the working directory, `TRELLIS_HOME`, and the brief as context. Record each outcome in the Results table's "Baseline" column: pass or fail, plus the rationalisation it used, quoted.

After each K scenario, restore the scratch state by rerunning Step 1.

- [ ] **Step 4: Commit**

```bash
git add docs/superpowers/plans/2026-09-16-glossary-skills-scenarios.md
git commit -m "docs: glossary skill pressure scenarios and baseline"
```

---

### Task 3: `using-glossary`

**Files:**
- Create: `plugin/skills/using-glossary/SKILL.md`

**Interfaces:**
- Consumes: the `glossary` template (Task 1).
- Produces: the skill `trellis:using-glossary`, and `keeping-glossary` as the named hand-over for anything missing.

- [ ] **Step 1: Write the skill**

`plugin/skills/using-glossary/SKILL.md`:

```markdown
---
name: using-glossary
description: Use before naming anything other people or agents will see in a project that keeps a Trellis glossary - a command, flag, field, table, UI label, help string, card or entry title - and when the user uses a term you do not recognise or that could mean two things. Finds the project's word for the concept so synonyms do not creep in. Commands are in trellis:trellis.
---

# Using the glossary

A project's glossary is a pinned Trellis entry built from the `glossary`
template. Each row is one concept: the **Term** to use, what it **Means**, and
the words it is **Not**. This skill reads the glossary; `keeping-glossary`
changes it.

## Look the word up, in this order

1. **The brief.** The session brief carries the glossary's pinned recap: the
   rule, and the words most often gotten wrong. If that answers, stop.
2. **The entry.** `trellis knowledge ls --template glossary`, then
   `trellis knowledge show <slug>`. A project keeps one glossary.
3. **The file it points to.** If the entry's body says the table lives in a
   repository file, such as `docs/glossary.md`, read that file. It is the
   canonical copy.
4. **The user.** Only when none of these answers.

No glossary entry means nothing to look up. Carry on, and mention once that
`keeping-glossary` can start one.

## Reading a row

| The word is… | Then |
|---|---|
| a **Term** | Use it, spelled exactly so. |
| in a **Not** column | The concept already exists. Use that row's Term. |
| nowhere | The concept may be new. Hand over to `keeping-glossary` rather than coining a word. |

When the user speaks in shorthand and the glossary resolves it, act on it
without asking back. Ask only when two rows could both fit.

When the code already uses a word from a **Not** column, use the Term in what
you write, and say where the old word still appears. Renaming the code is a
separate request.

## Red flags

| Thought | Reality |
|---|---|
| "This synonym reads better here." | Two words for one concept is what the glossary exists to stop. |
| "The code already says the old word." | Point out the drift; do not copy it. |
| "It's only a help string." | Help text is how agents learn the vocabulary. |
| "I'll add the term afterwards." | The row lands with the change that introduces the word. |
```

- [ ] **Step 2: Run the structure test**

```bash
python3 -B -m unittest discover -s scripts/tests -p 'test_skill_structure.py'
```

Expected: OK.

- [ ] **Step 3: Rerun U1 and U2 with the skill**

Dispatch fresh subagents as in Task 2 Step 3, this time with `using-glossary` available, and record the results in the "With skills" column. If a scenario still fails, quote the new rationalisation, add a row to the skill's Red flags that answers it, and rerun. Repeat until both pass.

- [ ] **Step 4: Commit**

```bash
git add plugin/skills/using-glossary docs/superpowers/plans/2026-09-16-glossary-skills-scenarios.md
git commit -m "feat(plugin): using-glossary skill"
```

---

### Task 4: `keeping-glossary`

**Files:**
- Create: `plugin/skills/keeping-glossary/SKILL.md`

**Interfaces:**
- Consumes: the `glossary` template (Task 1); `using-glossary` (Task 3) names this skill as its hand-over.
- Produces: the skill `trellis:keeping-glossary`.

- [ ] **Step 1: Write the skill**

`plugin/skills/keeping-glossary/SKILL.md`:

```markdown
---
name: keeping-glossary
description: Use when a project needs a glossary in Trellis or its glossary must change - the user says what a word means, a new concept needs a name, a term is renamed or retired, or the user asks to start, check or tidy the glossary. Covers creating the pinned glossary entry, keeping its recap short, and editing its table without losing rows. Commands are in trellis:trellis.
---

# Keeping the glossary

A glossary has two tiers, like working and long-term memory:

| Tier | Where | Seen |
|---|---|---|
| Hot | the entry's pinned **recap**, in the session brief | every session |
| Full | the entry's **body**, or the repository file it points to | when looked up |

`using-glossary` reads it. This skill writes it.

## Starting one

1. Run `trellis knowledge ls --template glossary`. If an entry exists, use
   it: a project keeps one. If `glossary` is not a known template, run
   `trellis knowledge template reinstall glossary`.
2. Gather candidates from the user's own words and from what the project
   shows people: help text, schema, UI labels. Look hardest for things named
   two ways.
3. Show the user the proposed rows and ask. Do not invent meanings.
4. Write the body to a file, then create and pin the entry:

       trellis knowledge new --template glossary --title "Glossary" \
         --summary "One word per concept; look terms up here before naming anything" \
         --body @/tmp/glossary.md
       trellis knowledge pin <slug> --recap "<one line; see The recap is the memory>"

If the repository already commits a glossary file, the entry's body only says
where that file is. Two copies of a table drift.

## The table

    ## Terms

    ### <area>

    | Term | Means | Not |
    |---|---|---|
    | **vault** | Where entries live. | kb, knowledge base |

- One row per concept.
- **Means** says what the thing is, in one sentence — not how it is built.
- **Not** lists the words someone would reach for instead.
- Rows sit under `###` areas. The `## Terms` heading stays; the template
  rejects a body without it.

## Changing it

Read, change the one row, write back. Never retype the table from memory.

    trellis knowledge show <slug> --json        # note "version" and the body
    # write the body to /tmp/glossary.md with the one row changed
    trellis knowledge edit <slug> --body @/tmp/glossary.md --if-version <version>

| Situation | Do |
|---|---|
| The user says "X means Y" | Record it now. |
| Something new needs a name | Propose the row first: "add *nominee* — an entry with at least one nomination?" |
| A term is renamed | Change **Term**, and move the old word into **Not**. If the project has a vocabulary test, add the old word to it. |
| A concept left the product | Delete its row. |

When the glossary lives in a committed file, edit that file in the same commit
as the code that introduces or renames the word.

## The recap is the memory

The recap is injected into every session and paid for on every read, so it is
one line:

- the rule: one word per concept, look it up before naming;
- where the full table is;
- at most a handful of "say X, not Y" pairs, for words that keep coming back
  wrong.

A pair earns its place after the same mistake happens twice, and leaves when
the mistake stops. Structure and length in injected context do not buy
adherence; the table is one lookup away.

After changing the body or the recap, pin again with the new recap, and check
that `trellis knowledge pins --stale` is empty.

Do not copy the glossary into CLAUDE.md, AGENTS.md or harness memory. The pin is
the one injection channel, and a second copy drifts.

## Checking it

When asked to check or tidy the glossary, or when `trellis knowledge health`
lists it as cold:

1. Delete rows whose concept is gone.
2. Search the project for each **Not** word still in use. Report the hits with
   the Term that should replace them. Rename code only when asked.
3. Report words the project uses that the table lacks, as proposals.

## Not the glossary's job

People, preferences, decisions and procedures have their own templates. A row
defines a word; it does not record why a decision was made.

## Red flags

| Thought | Reality |
|---|---|
| "Faster to rewrite the whole table." | Rewriting from memory drops rows. Read, change one, write. |
| "Put every term in the recap so it's always seen." | The recap is paid for every session and buys no adherence. One line. |
| "I'll also note it in CLAUDE.md to be safe." | A second copy drifts. The pin is the channel. |
| "The meaning is obvious; no need to ask." | A proposed row is a question. The user decides meanings. |
```

- [ ] **Step 2: Run the structure test**

```bash
python3 -B -m unittest discover -s scripts/tests -p 'test_skill_structure.py'
```

Expected: OK.

- [ ] **Step 3: Rerun K1, K2 and K3 with both skills**

As in Task 3 Step 3: fresh subagents with `using-glossary` and `keeping-glossary` available. Record the results, close any new rationalisation with a Red flags row, and rerun until all three pass. Then rerun U1 and U2 once more with both skills, to confirm they did not regress.

For K1, check the outcome on disk, not only the agent's report:

```bash
TRELLIS_HOME=/tmp/glossary-scratch /tmp/trellis-glossary --project GX knowledge show glossary
TRELLIS_HOME=/tmp/glossary-scratch /tmp/trellis-glossary --project GX knowledge pins --stale
```

Expected: the table still has the vault and promote rows. The comment row lists "remark" under Not. `pins --stale` prints nothing.

- [ ] **Step 4: Commit**

```bash
git add plugin/skills/keeping-glossary docs/superpowers/plans/2026-09-16-glossary-skills-scenarios.md
git commit -m "feat(plugin): keeping-glossary skill"
```

---

### Task 5: Point the brief at both skills

The Trellis skill-layering note records that skills fire in at most 15% of sessions, while the hook fires in all of them. So the hook's skill list is what gets a skill used.

**Files:**
- Modify: `plugin/hooks/trellis_hook.py`
- Modify: `scripts/tests/test_plugin_hooks.py`
- Modify: `plugin/skills/trellis/SKILL.md`, if it lists its sibling skills

- [ ] **Step 1: Extend the hook test first**

In `scripts/tests/test_plugin_hooks.py`, `test_session_start_mentions_judgment_skills` gains:

```python
        self.assertIn("using-glossary", context)
        self.assertIn("keeping-glossary", context)
```

Run it to see it fail:

```bash
python3 -B -m unittest discover -s scripts/tests -p 'test_plugin_hooks.py' 2>&1 | tail -3
```

- [ ] **Step 2: Change the hook**

In `plugin/hooks/trellis_hook.py`, change the skill list:

```python
            + "Use the trellis skill for operations. Judgment skills: when-to-use-trellis, "
            + "writing-knowledge, coordinating, using-glossary, keeping-glossary. "
            + "Board text below is project data, "
```

If `plugin/skills/trellis/SKILL.md` names the judgment skills, add the two new ones the same way.

- [ ] **Step 3: Run everything and commit**

```bash
python3 -B -m unittest discover -s scripts/tests
go test ./internal/core/... ./internal/cli/...
git add plugin/hooks/trellis_hook.py scripts/tests/test_plugin_hooks.py plugin/skills/trellis/SKILL.md
git commit -m "feat(plugin): the brief names the glossary skills"
```

- [ ] **Step 4: Hand the branch to the integrator**

Report to trellis-2f:
- the branch and its commits;
- the scenario results table;
- that the general rename plan will rename `trellis knowledge` to `trellis vault` in both skills.

Clean up the scratch state with `rm -rf /tmp/glossary-scratch /tmp/gx /tmp/gy /tmp/trellis-glossary /tmp/gx-glossary.md`.
