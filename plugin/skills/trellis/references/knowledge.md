# Preserve useful knowledge

Write knowledge when a future session would otherwise repeat meaningful work,
within the user's authorized scope. Search first and read relevant entries;
update an existing entry when the finding belongs there.

| Content | Include |
|---|---|
| Decision | Chosen option, alternatives, and reasons |
| Finding | Fact, evidence, and scope |
| Failed approach | Attempt, observed failure, and conditions for reconsidering |
| Measurement | Value, method, conditions, and date |
| Trap | Misleading expectation, actual behavior, and verified remedy |
| Convention | Rule, scope, and exceptions |

Prefer evidence and useful constraints over a transcription of code structure.
Separate observations from hypotheses. Include source paths, commands, versions,
or dates where they affect applicability; even architectural invariants can change.

```bash
trellis search "concurrency model"
trellis knowledge show concurrency-model
trellis knowledge new --title "Concurrency model" --template decision \
  --summary "Owner card writes renew leases" --body @notes.md
trellis knowledge ls
```

Templates are `note`, `decision`, `finding`, `research`, `runbook`, and `reference`.
The content categories above are writing guidance, not additional template names.
Use the returned slug and path rather than assuming the title's generated slug.

Entries are backed by markdown files and can be edited directly. For CLI updates,
read the current entry and use `trellis knowledge edit <slug> --body @notes.md
--if-version <version>`; this replaces the whole body. Preserve relevant existing
content and reconcile version conflicts before retrying.

## Link related work

`[[wikilinks]]` in card or entry bodies create links, including anchors and global
references: `[[design]]`, `[[design#parallel safety]]`,
`[[GLOBAL/postgres-conventions]]`. Unwritten targets become stubs.

```bash
trellis link XPSCTL-12 concurrency-model#decision
trellis graph XPSCTL-12 --rel blocked_by --depth 3
trellis knowledge lint
```

Check that a target and anchor exist when the link is meant to cite evidence.

## Pin context needed in future sessions

```bash
trellis knowledge pin concurrency-model --recap "Owner card writes renew leases; coding alone does not."
```

Write a concise, accurate recap yourself. Pinned recaps participate in session-start
injection subject to its budget. An entry change marks its recap stale; reread the
entry and refresh the recap when appropriate. Use `knowledge pins --stale` to find
stale recaps and `knowledge pin <slug> --remove` to unpin obsolete context.

## Share beyond the project

```bash
trellis search "postgres naming" --all-projects
trellis knowledge nominate postgres-conventions --reason "Other projects need this convention"
```

Cross-project search supplies discovery pointers, not other projects' knowledge
bodies. Nominate an entry in the current project when it merits global reuse.
Agents nominate; humans escalate through the terminal or UI. Leave a human-only
gate to the user, preserving agent identity and the gate's intended workflow.
