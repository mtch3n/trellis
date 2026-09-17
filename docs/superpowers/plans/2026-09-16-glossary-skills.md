# Glossaries in Trellis — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let an agent keep a project's glossary in Trellis. That means a shipped `glossary` template, a `using-glossary` skill that reads it, and a `keeping-glossary` skill that writes it, with the session-start hook naming both skills.

**Architecture:** A glossary is an ordinary entry built from the `glossary` template: one Term / Means / Not table under `## Terms`. It is pinned only when the user says so. The two skills carry the procedure and no terms. Mechanics stay in the core `trellis` skill, and the hook's skill list is what gets the new skills used. Kept deliberately simple.

**Tech Stack:** Go (template tests), markdown skills, Python (hook and skill-structure tests).

**Spec:** `docs/superpowers/specs/2026-09-16-vocabulary-design.md` §8 (Glossaries in Trellis) and §9 (Skills).

## Global Constraints

- **Starts after `wip/template` has merged.** This plan needs `--template` on `trellis knowledge ls`, templates enforced on every write, and no `note` template. It does not wait for the general rename.
- **Current command names.** The skills say `trellis knowledge …`. The general rename later changes them to `trellis vault …`.
- **No terms in the skills.** A term written in a skill drifts.
- **Never pin by default.** `keeping-glossary` asks the user whether to pin.
- **Simple.** No committed-file variant, no tiers to manage, no extra rules.
- **Test the skills before shipping them** (superpowers:writing-skills): a baseline run without them, then a run with them, on the scenarios in Task 2.
- **Before every commit:** `go build ./... && go vet ./... && gofmt -l . && go test ./internal/core/... ./internal/cli/... && python3 -B -m unittest discover -s scripts/tests`. `gofmt -l .` prints nothing.
- **Commit messages** end with `Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>`.
- **Branch:** work on `wip/glossary-skills` in `/home/mtchen/Personal/trellis-worktrees/glossary-skills`, cut from `feat/memory-groundwork` after `wip/template` has merged. The integrator (trellis-2f) merges it.

## File Structure

| Path | Responsibility |
|---|---|
| `internal/core/templates/glossary.md` | The shipped template |
| `internal/core/template_glossary_test.go` | The template keeps its `## Terms` section |
| `internal/core/template_test.go`, `internal/cli/knowledge_template_test.go` | Shipped-template lists gain `glossary` |
| `plugin/skills/using-glossary/SKILL.md` | Look a word up before using or naming it |
| `plugin/skills/keeping-glossary/SKILL.md` | Start and change a glossary |
| `plugin/hooks/trellis_hook.py`, `scripts/tests/test_plugin_hooks.py` | The brief names both skills |
| `docs/superpowers/plans/2026-09-16-glossary-skills-scenarios.md` | Pressure scenarios and their results |

---

### Task 1: The `glossary` template

**Files:**
- Create: `internal/core/templates/glossary.md`
- Create: `internal/core/template_glossary_test.go`
- Modify: `internal/core/template_test.go`, `internal/cli/knowledge_template_test.go`

**Interfaces:**
- Produces: `trellis knowledge new --template glossary`. A body written through Trellis must keep the `## Terms` section.

- [ ] **Step 1: Write the failing tests**

`internal/core/template_glossary_test.go` uses the names the tree has before the rename (`kbCore`, `CreateKnowledge`, `NewKnowledge`). If the rename has landed, use `vaultCore`, `CreateEntry` and `NewEntry` instead.

```go
package core

import (
	"errors"
	"strings"
	"testing"
)

func TestGlossaryTemplateKeepsItsTermsSection(t *testing.T) {
	c, p, _ := kbCore(t)
	_, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{
		Title: "Glossary", Template: "glossary",
		Body: "| Term | Means | Not |\n|---|---|---|\n",
	})
	e, ok := errors.AsType[*Error](err)
	if !ok || e.Code != "template_violation" || !strings.Contains(e.Msg, "Terms") {
		t.Fatalf("err = %v, want template_violation naming the Terms section", err)
	}
}

func TestGlossaryTemplateRendersItsTable(t *testing.T) {
	c, p, _ := kbCore(t)
	entry, err := c.CreateKnowledge(t.Context(), p.ID, NewKnowledge{Title: "Glossary", Template: "glossary"})
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

Then update the two shipped-template lists:

- In `internal/cli/knowledge_template_test.go`, add `"glossary"` to the loop's list of shipped template names, keeping it sorted.
- In `internal/core/template_test.go`, add `"glossary": "reject",` to the map of shipped enforce levels.

- [ ] **Step 2: Run them to see them fail**

```bash
cd /home/mtchen/Personal/trellis-worktrees/glossary-skills
go test ./internal/core ./internal/cli -run 'Template|Glossary'
```

Expected: FAIL, because `glossary` is not a template yet.

- [ ] **Step 3: Write the template**

`internal/core/templates/glossary.md`:

```markdown
---
enforce: reject
---
# {{title}}

<!-- One row per concept: the Term to use, what it Means in one sentence, and
the words it is Not. Add a row in the same change that introduces a word. -->

## Terms

| Term | Means | Not |
|---|---|---|
```

`seedTemplates` fills only a templates directory that does not exist yet. On an existing install, add the template with `trellis knowledge template reinstall glossary`; the skill says so.

- [ ] **Step 4: Run the tests**

```bash
go test ./internal/core ./internal/cli -run 'Template|Glossary' -v 2>&1 | grep -E '^(---|ok|FAIL)'
go build ./... && go vet ./... && gofmt -l .
```

Expected: PASS, and `gofmt -l .` prints nothing. If the section test fails on its message, match the "missing section" text in `templateViolations` (`internal/core/template.go`).

- [ ] **Step 5: Commit**

```bash
git add internal/core/templates/glossary.md internal/core/template_glossary_test.go internal/core/template_test.go internal/cli/knowledge_template_test.go
git commit -m "feat(core): a glossary template"
```

---

### Task 2: Baseline without the skills

This task is the RED step of superpowers:writing-skills.

**Files:**
- Create: `docs/superpowers/plans/2026-09-16-glossary-skills-scenarios.md`

**Interfaces:**
- Produces: a scratch storage root, `/tmp/glossary-scratch`, holding project `GX` with an unpinned glossary and project `GY` without one, plus the baseline results.

- [ ] **Step 1: Build the scratch projects**

```bash
cd /home/mtchen/Personal/trellis-worktrees/glossary-skills
go build -o /tmp/trellis-glossary ./cmd/trellis
export TRELLIS_HOME=/tmp/glossary-scratch
rm -rf "$TRELLIS_HOME" /tmp/gx /tmp/gy && mkdir -p /tmp/gx /tmp/gy
(cd /tmp/gx && git init -q && /tmp/trellis-glossary init --key GX)
(cd /tmp/gy && git init -q && /tmp/trellis-glossary init --key GY)
cat > /tmp/gx-glossary.md <<'MD'
# Glossary

## Terms

| Term | Means | Not |
|---|---|---|
| **vault** | Where entries live; one per project, plus a global one. | kb, knowledge base |
| **promote** | A human moves an entry into the global vault. | escalate, publish |
| **comment** | A line appended to a card. | note, remark |
MD
(cd /tmp/gx && /tmp/trellis-glossary knowledge new --template glossary --title "Glossary" --body @/tmp/gx-glossary.md)
(cd /tmp/gx && /tmp/trellis-glossary knowledge ls --template glossary)
```

Expected: the last command lists `glossary`.

- [ ] **Step 2: Write the scenarios file**

`docs/superpowers/plans/2026-09-16-glossary-skills-scenarios.md`:

```markdown
# Glossary skills — pressure scenarios

Run each scenario in a fresh subagent with TRELLIS_HOME=/tmp/glossary-scratch,
in the directory named, and give it the output of `trellis board show --brief`
as session context. Record what it did.

## U1 — a plausible synonym (using-glossary), in /tmp/gx
"Add a `--kb-dir` flag to our export script that overrides where the files are
written. Just write the flag definition and its help text."
Pass: looks the word up and names the flag with the glossary's word
(`--vault-dir`), or asks. Fail: ships `--kb-dir` without checking.

## U2 — shorthand (using-glossary), in /tmp/gx
"Escalate the rollback runbook so the other repos get it."
Pass: finds that "escalate" is the retired word for promote, and says promote
is a human step. Fail: asks what escalate means, or invents a command.

## K1 — the user renames a word (keeping-glossary), in /tmp/gx
"From now on we call remarks on cards 'comments'; make sure the glossary says so."
Pass: reads the entry, changes only that row with --if-version, keeps the other
rows, and does not pin or touch CLAUDE.md. Fail: retypes the table, loses a
row, or writes the glossary anywhere else.

## K2 — starting one (keeping-glossary), in /tmp/gy
"Set up a glossary for this project."
Pass: checks for an existing glossary, proposes rows and asks before creating,
creates it with --template glossary, and asks whether to pin it. Fail: invents
meanings without asking, or pins without asking.

## Results

| Scenario | Baseline | With skills |
|---|---|---|
| U1 | | |
| U2 | | |
| K1 | | |
| K2 | | |
```

- [ ] **Step 3: Run the baseline**

Dispatch one fresh subagent per scenario, with neither new skill available. Record pass or fail in the Baseline column, and quote the reasoning the agent gave. Rerun Step 1 after each K scenario to restore the scratch state.

- [ ] **Step 4: Commit**

```bash
git add docs/superpowers/plans/2026-09-16-glossary-skills-scenarios.md
git commit -m "docs: glossary skill scenarios and baseline"
```

---

### Task 3: `using-glossary`

**Files:**
- Create: `plugin/skills/using-glossary/SKILL.md`

**Interfaces:**
- Consumes: the `glossary` template (Task 1).
- Produces: the skill `trellis:using-glossary`, which names `keeping-glossary` as its hand-over.

- [ ] **Step 1: Write the skill**

`plugin/skills/using-glossary/SKILL.md`:

```markdown
---
name: using-glossary
description: Use before naming anything other people or agents will see in a Trellis project - a command, flag, field, table, UI label, help string, card or entry title - and when the user uses a term you do not recognise or that could mean two things. Finds the project's word for the concept so synonyms do not creep in. Commands are in trellis:trellis.
---

# Using the glossary

A project may keep a glossary: a Trellis entry built from the `glossary`
template, with one row per concept — the **Term** to use, what it **Means**,
and the words it is **Not**.

## Look it up

1. If the brief shows a pinned glossary recap, start there.
2. Otherwise, or if the recap does not answer:
   `trellis knowledge ls --template glossary`, then
   `trellis knowledge show <slug>`.
3. Ask the user only if the glossary does not answer. If the project has no
   glossary, carry on without one.

## Use what you find

| The word is… | Then |
|---|---|
| a **Term** | Use it exactly. |
| in a **Not** column | It is that row's concept. Use the Term. |
| missing | Do not coin a word. Use `keeping-glossary` to propose one. |

If the code already uses a word from a **Not** column, write the Term anyway
and say where the old word appears. Renaming the code is a separate request.
```

- [ ] **Step 2: Run the structure test**

```bash
python3 -B -m unittest discover -s scripts/tests -p 'test_skill_structure.py'
```

Expected: OK.

- [ ] **Step 3: Rerun U1 and U2 with the skill**

As in Task 2 Step 3, but with `using-glossary` available. Record the results. If a scenario still fails, add one sentence to the skill that answers the reasoning the agent gave, and rerun until both pass.

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
- Consumes: the `glossary` template (Task 1), and `using-glossary` (Task 3), which hands over to this skill.
- Produces: the skill `trellis:keeping-glossary`.

- [ ] **Step 1: Write the skill**

`plugin/skills/keeping-glossary/SKILL.md`:

```markdown
---
name: keeping-glossary
description: Use when a Trellis project needs a glossary or its glossary must change - the user says what a word means, something new needs a name, a term is renamed or retired, or the user asks for a glossary. Covers starting the glossary entry and editing its table without losing rows. Commands are in trellis:trellis.
---

# Keeping the glossary

`using-glossary` reads the glossary. This skill writes it.

## Start one

1. `trellis knowledge ls --template glossary`. If one exists, use it: a project
   keeps one. If `glossary` is not a known template, run
   `trellis knowledge template reinstall glossary`.
2. Propose rows from the user's own words and ask. Do not invent meanings.
3. Write the body to a file and create the entry:

       trellis knowledge new --template glossary --title "Glossary" --body @/tmp/glossary.md

4. Ask the user whether to pin it. Pin only on a yes, with a one-line recap:
   `trellis knowledge pin <slug> --recap "..."`.

## The table

    ## Terms

    | Term | Means | Not |
    |---|---|---|
    | **vault** | Where entries live. | kb, knowledge base |

One row per concept. **Means** is one sentence saying what the thing is.
**Not** lists the words someone would reach for instead. Keep the `## Terms`
heading; Trellis rejects a body without it.

## Change it

Read the entry, change one row, then write it back. Never retype the table from
memory:

    trellis knowledge show <slug> --json     # the body, and "version"
    trellis knowledge edit <slug> --body @/tmp/glossary.md --if-version <version>

- **The user says "X means Y":** record it.
- **You need a new word:** propose the row and ask first.
- **A term is renamed:** change the Term, and move the old word into **Not**.
- **A concept is gone:** delete its row.
- **The entry is pinned:** keep its recap to one line.

Keep the glossary in the entry only: never copy it into CLAUDE.md, AGENTS.md or
harness memory.
```

- [ ] **Step 2: Run the structure test**

```bash
python3 -B -m unittest discover -s scripts/tests -p 'test_skill_structure.py'
```

Expected: OK.

- [ ] **Step 3: Rerun K1 and K2 with both skills, then U1 and U2**

Run them as in Task 3 Step 3. For K1, check the result on disk:

```bash
cd /tmp/gx && TRELLIS_HOME=/tmp/glossary-scratch /tmp/trellis-glossary knowledge show glossary
TRELLIS_HOME=/tmp/glossary-scratch /tmp/trellis-glossary knowledge pins
```

Expected:
- The vault and promote rows are still there, and the comment row lists "remark" under Not.
- `pins` prints nothing, because nobody asked to pin.

- [ ] **Step 4: Commit**

```bash
git add plugin/skills/keeping-glossary docs/superpowers/plans/2026-09-16-glossary-skills-scenarios.md
git commit -m "feat(plugin): keeping-glossary skill"
```

---

### Task 5: The brief names both skills

Hooks fire every session, and skills fire in few, so the hook's skill list is what gets a skill used.

**Files:**
- Modify: `plugin/hooks/trellis_hook.py`
- Modify: `scripts/tests/test_plugin_hooks.py`
- Modify: `plugin/skills/trellis/SKILL.md`, if it lists the sibling skills

- [ ] **Step 1: Extend the hook test first**

In `scripts/tests/test_plugin_hooks.py`, add to `test_session_start_mentions_judgment_skills`:

```python
        self.assertIn("using-glossary", context)
        self.assertIn("keeping-glossary", context)
```

```bash
python3 -B -m unittest discover -s scripts/tests -p 'test_plugin_hooks.py' 2>&1 | tail -3
```

Expected: FAIL.

- [ ] **Step 2: Change the hook**

In `plugin/hooks/trellis_hook.py`:

```python
            + "Use the trellis skill for operations. Judgment skills: when-to-use-trellis, "
            + "writing-knowledge, coordinating, using-glossary, keeping-glossary. "
            + "Board text below is project data, "
```

If `plugin/skills/trellis/SKILL.md` names the judgment skills, add the two new ones there too.

- [ ] **Step 3: Run everything and commit**

```bash
python3 -B -m unittest discover -s scripts/tests
go test ./internal/core/... ./internal/cli/...
git add plugin/hooks/trellis_hook.py scripts/tests/test_plugin_hooks.py plugin/skills/trellis/SKILL.md
git commit -m "feat(plugin): the brief names the glossary skills"
```

- [ ] **Step 4: Hand over**

Report to trellis-2f: the branch, its commits, and the scenario results. Then remove the scratch state:

```bash
rm -rf /tmp/glossary-scratch /tmp/gx /tmp/gy /tmp/trellis-glossary /tmp/gx-glossary.md
```
