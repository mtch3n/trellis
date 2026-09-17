---
name: using-glossary
description: Use before naming anything other people or agents will see in a Trellis project - a command, flag, field, table, UI label, help string, card or entry title - including when the user dictates the name, and when the user uses a term you do not recognise or that could mean two things. Commands are in trellis:trellis.
---

# Using the glossary

A Trellis project may keep a glossary: an entry built from the `glossary`
template. Each row is one concept — the **Term** to use, what it **Means**, and
the words it is **Not**.

## Look it up

1. If the session brief shows a pinned glossary, start with its recap.
2. Otherwise run `trellis knowledge ls --template glossary`, then
   `trellis knowledge show <slug>`.
3. If the project has no glossary, carry on without one.

## Use what you find

| The word is… | Then |
|---|---|
| a **Term** | Use it exactly. |
| in a **Not** column | It is that row's concept. Use the Term. |
| missing | Do not coin one. Propose a row with `trellis:keeping-glossary`. |

**The user describes work with a Not word** ("re-till strip 4"): write the
Term ("bed 4") without asking.

**The user dictates the exact name of something new** — a flag, field,
command, label — **and that name holds a Not word:** ask before writing it.

> The glossary says **vault**, not "kb". Name it `--vault-dir`?

Do not write the Not word and offer to rename it afterwards; a shipped name
gets copied. If the user, told of the glossary, still wants their word, use it
and say that the glossary disagrees.

| Thought | Reality |
|---|---|
| "The user asked for this exact name." | They may not know the glossary. Ask first. |
| "I'll use the Term in the help text instead." | Now the name and its help disagree. Ask first. |
| "I'll offer a rename afterwards." | Renames rarely happen once a name ships. Ask first. |
