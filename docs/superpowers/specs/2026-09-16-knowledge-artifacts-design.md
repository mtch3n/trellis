# Knowledge artifacts

Date: 2026-09-16
Status: design, approved for implementation
Ships after: `2026-09-16-knowledge-disclosure-policy-design.md`
Frontend: built in parallel by another session against the contract below.

## Problem

Knowledge means every markdown document a project keeps, and many of them come
with a file: a meeting note and its recording, a research note and the PDF it
summarises, a runbook and its architecture diagram.

Trellis already stores files — `trellis artifact add` copies images, text,
audio, video, PDFs and archives into `<root>/projects/<KEY>/artifacts/`, and
SQLite holds only their metadata. But an artifact can be attached to a **card**
and nothing else. `LinkArtifactToCard` is the only way to create a link, and the
schema comment in `0009_artifacts.sql` says so. A knowledge document cannot
refer to one, and the web UI cannot show or serve one.

## What this adds

1. A knowledge document names its artifacts in its frontmatter.
2. The CLI links and unlinks artifacts to documents as well as cards.
3. The web API lists a document's artifacts and serves an artifact's bytes for
   preview, safely.

## The reference lives in frontmatter

```yaml
---
title: Standup 2026-09-16
type: note
artifacts: [standup-2026-09-16.mp3]
---
```

- **The file stays the source of truth.** The list is in the markdown, readable
  and editable in any editor.
- **`artifacts` is a first-class `Frontmatter` field**, `[]string`, like `tags`
  and `labels`. It does not depend on the templates design's preservation of
  unknown keys.
- **Entries are artifact names** — the stored filename, such as
  `standup-2026-09-16.mp3`. A name is readable, and within a project it
  identifies one artifact (see below).

## Links are derived, like wikilinks

`syncDocRelations` (`internal/core/doc_relations.go`) already replaces a
document's derived wikilink rows every time the file changes. Artifact
references join the same pass:

- Delete the document's rows with `rel = 'artifact'`.
- For each name in `fm.Artifacts`, insert
  `('doc', doc.ID, 'artifact', <artifact id or NULL>, <name>, NULL, 'artifact')`.
- A name that resolves to nothing is stored with `to_id` NULL — a **stub**,
  exactly as a wikilink to a document that does not exist yet.

A hand edit in any editor that adds or removes a name changes the content hash,
so the next read refreshes the file and rewrites the rows.

Resolution happens within the document's own project.

### Names are unique, and must stay so

Resolution is by name, so a name must identify one artifact per project.

Today that holds by construction: every artifact of a project is written into
one directory, and `CreateArtifact` suffixes `-2`, `-3` when a filename is
taken. It is not guaranteed, though. The collision loop checks only the
filesystem, and `UNIQUE (project_id, path)` constrains the full path — if the
storage root has moved between two `artifact add` calls, two rows can share a
name with different paths.

- **`CreateArtifact` also checks the database** for the name within the project
  before accepting it, so no new duplicate can be created.
- **No unique index is added.** A migration adding one would fail on a database
  that already holds a duplicate, and a failed migration stops Trellis from
  starting. That is too severe a failure for a rare case.
- **A name matching more than one artifact resolves to nothing** and is
  reported by lint, rather than silently picking one.

### Creating and deleting artifacts

- **`CreateArtifact` backfills stubs.** After inserting the row it sets `to_id`
  on this project's document links whose `to_raw` is the new name and whose
  `to_id` is NULL — the same thing `resolveDocStubs` does for documents.
- **`DeleteArtifact` turns document links into stubs rather than deleting
  them.** Before the artifact row is removed, it sets `to_id` to NULL on
  document links pointing at it. The `artifact_links_ad` trigger then deletes
  only card links, whose reference lives nowhere but the database. A document
  still names the artifact in its file, so its link stays, dangling — which is
  what the project does for a wikilink to a deleted document.

## CLI

```
trellis artifact add <file> [--card <ref> | --doc <slug>]
trellis artifact link   <artifact> (--card <ref> | --doc <slug>)
trellis artifact unlink <artifact> (--card <ref> | --doc <slug>)
trellis artifact ls [--card <ref> | --doc <slug>]
```

- `<artifact>` is an artifact's id **or** its name. Ids are UUIDs and names are
  filenames, so the two cannot collide.
- Exactly one of `--card` and `--doc` is required for `link` and `unlink`.
- **`link --doc` and `unlink --doc` edit the document's frontmatter**, through
  the same load–modify–`writeAtomic` path that `knowledge edit` uses, and the
  derived rows follow from that write. Linking a name already listed, or
  unlinking one that is not, is a no-op, not an error.
- `unlink` is new for cards too. A link that cannot be removed is a trap.
- Linking or unlinking a document records an `artifact_linked` or
  `artifact_unlinked` event whose value is the artifact name.
- `knowledge show` includes the document's artifacts, because they are part of
  the document's computed view.

## Lint

A new finding kind, `missing_artifact`, for a document artifact link whose
`to_id` is NULL. Its detail says whether the name matches no artifact or more
than one. Its fix is `trellis artifact add <file>` or `trellis artifact ls`
respectively.

The existing `stub` finding queries `rel = 'wikilink'` only (`lint.go`), so
artifact stubs do not leak into it.

## Web API

### Artifacts on knowledge items

Every item returned by `GET /api/p/{key}/knowledge` and
`GET /api/p/{key}/b/{board}/knowledge` gains:

```json
"artifacts": [
  {
    "name": "standup-2026-09-16.mp3",
    "kind": "audio",
    "mime": "audio/mpeg",
    "size": 2048331,
    "url": "/api/p/TRELLIS/artifacts/standup-2026-09-16.mp3",
    "missing": false
  }
]
```

- `kind` is `image`, `audio`, `video`, `document`, `text` or `archive` — the
  values `artifactKind` already produces.
- A stub is `{"name": "...", "missing": true}`, with no other fields.
- The field is omitted for a document with no artifacts.
- `GET /api/global/knowledge` never carries artifacts; it selects rows directly
  and an artifact belongs to a project.

`core.Knowledge` gains a computed `Artifacts` field, filled in `docView` like
`Ref` and `Backlinks`, carrying name, kind, MIME, size and the missing flag.
**Core does not know UI URLs.** The UI layer adds `url` when it builds the
response.

### Serving an artifact

`GET /api/p/{key}/artifacts/{name}`

The endpoint sits under `/api/`, so it inherits everything `protectedHandler`
(`internal/ui/security.go`) applies to the whole mux: a `Host` that is not
`localhost` or a loopback IP on the daemon's port is refused, which is the
DNS-rebinding defence; a mismatched `Origin` and `Sec-Fetch-Site: cross-site`
are refused; the session token is required, from the `X-Trellis-Token` header
or the HttpOnly, SameSite=Strict `trellis_session` cookie; and every response
already carries `nosniff`, `Referrer-Policy: no-referrer` and
`Cache-Control: no-store`. The daemon refuses any non-loopback bind. Nothing
below repeats or weakens those.

1. Resolve the project by key; 404 if it does not exist.
2. Resolve the artifact by project and name; 404 if there is no match or more
   than one. **Only registered artifacts are served** — never an arbitrary file
   that happens to sit in the directory.
3. Confirm the stored path lies inside that project's artifact directory. The
   path comes from the database, so this is defence in depth, but a row edited
   or restored from elsewhere must not become a way to read any file on disk.
4. Open the file; 404 if it is gone from disk.
5. Set the headers below, then hand the file to `http.ServeContent`, which
   answers Range requests (so audio and video can seek), conditional requests
   and HEAD.

`Content-Type` must be set **before** `ServeContent` is called, or it will sniff
the bytes and choose its own.

## Serving rules

Artifacts are arbitrary user files served from the web UI's own origin, and the
accepted types include `text/html` and `image/svg+xml` — both can carry script.
Served naively, an uploaded HTML or SVG file is stored cross-site scripting
against the UI.

| Stored type | `Content-Type` sent | Extra |
|---|---|---|
| `image/svg+xml` | `image/svg+xml` | |
| other `image/*` | stored type | |
| `audio/*`, `video/*` | stored type | |
| `application/pdf` | `application/pdf` | **no** `sandbox` |
| `text/*`, including `text/html` | `text/plain; charset=utf-8` | |
| archive | stored type | `Content-Disposition: attachment` |

Every response carries:

- `X-Content-Type-Options: nosniff` — already set globally; set again here so
  the rule holds even if this handler is ever mounted elsewhere
- `Content-Security-Policy: sandbox` — **except PDF**
- `Content-Disposition: inline` with the filename, except archives, formatted
  with `mime.FormatMediaType` so a filename cannot inject a header

Why this is enough:

- **HTML never renders as HTML.** It is sent as plain text.
- **`sandbox` makes any document this endpoint returns an opaque origin**, so
  even if something renders as a document on direct navigation, its script
  cannot reach the UI's cookies, storage or DOM. A resource response's CSP
  applies only when it is rendered as a document; `<img>`, `<audio>` and
  `<video>` loads ignore it, so previews are unaffected.
- **PDF is exempt from `sandbox`** because browsers refuse to run their PDF
  viewer inside a sandboxed document. Browsers render PDF in an isolated viewer
  rather than as a page of the UI's origin, and `nosniff` stops a disguised
  file from being treated as anything else.

The frontend's half, in the contract given to it: SVG only through `<img>`,
never inlined; text only inside `<pre>`, never as HTML or markdown; archives as
a download link; media with `preload="metadata"`; everything loaded lazily.

A PDF is previewed in a same-origin `<iframe>`, and **the iframe loads only when
both `kind` is `document` and `mime` is `application/pdf`**; anything else falls
back to a download link. That makes the preview depend on two independent
fields rather than one server-side guarantee.

The iframe must **not** carry a `sandbox` attribute. An automated security
review recommended `sandbox=""` for this exact line; it would break preview,
because browsers refuse to start their PDF viewer in a sandboxed frame. The
safety comes from the response headers and the two-field check, not from the
attribute.

## Disclosure

This adds no automatic route for a body. Artifacts are not embedded, not
indexed and not injected, before or after this change.

- A private document's artifact list is returned like any other metadata on
  an explicit read (`knowledge show`, `knowledge ls`, the knowledge list
  endpoints). Presence is not what the disclosure design protects.
- The event log is not an explicit read. For a private document,
  `artifact_linked` and `artifact_unlinked` events are recorded with an empty
  value, as edits are, and marking a document private blanks the names its
  earlier artifact events recorded.
- A file's type comes from its bytes. The extension is consulted only when
  sniffing finds nothing, and never yields `application/pdf`: a real PDF always
  sniffs as one, so that fallback would only ever label non-PDF bytes as the
  one type served without `sandbox`.
- An artifact's bytes leave only on an explicit request for that artifact — the
  same standing as `knowledge show`.
- The UI is loopback-only and token-protected (see above), so serving bytes
  stays inside the trusted-local-caller boundary the disclosure design already
  assumes.
- An artifact has no `private` flag of its own. None is needed while no
  automatic route exists; it becomes necessary the day artifacts are embedded,
  transcribed or indexed.

## Accepted limitations

- **PDF is served without `sandbox` and previewed in a same-origin iframe.**
  That is safe only while the backend sends `application/pdf` with `nosniff` for
  every `document` artifact; if it ever sent something else, the iframe would
  run it with full access to the UI. The headers are therefore a pinned test,
  and the frontend refuses to iframe anything whose MIME is not
  `application/pdf`. The complete fix is serving artifacts from a separate
  origin, so that even content that executes cannot reach the UI's session; it
  is not planned.
- **The browser's PDF viewer must get past `protectedHandler`.** Chrome and
  Firefox load a PDF through their own viewer, which may issue further range
  requests for the same URL. If those requests arrive without the session
  cookie, or marked `Sec-Fetch-Site: cross-site`, the preview fails with 401 or
  403. This cannot be settled by a Go test; it is checked in a real browser
  before the feature is called done, and if it fails the fix belongs in the
  handler's treatment of that endpoint, not in removing the checks.
- **Derived link rows are not rebuilt from files after a database restore**
  unless the file's content hash differs. Pre-existing for wikilinks too, and
  part of the broader gap that no path reconstructs knowledge rows from files.

## Non-goals

- Embedding artifacts inline in a document body.
- Artifacts on global documents.
- Web endpoints that upload, link, unlink or delete. The UI reads; the CLI
  writes.
- Thumbnails, transcoding, waveforms, page counts, or any processing of an
  artifact's contents.
- A `private` flag on artifacts.
- Version history of documents or cards.

## Testing

- `syncDocRelations`: a document naming an existing artifact gets a resolved
  link; a name that does not exist gets a stub; removing a name from the file
  removes its row on the next refresh.
- A name shared by two artifacts in one project resolves to nothing and lint
  reports it as ambiguous.
- `CreateArtifact` refuses a name already present in the database for the
  project, even when no file of that name is on disk.
- `CreateArtifact` backfills a stub left by a document written first.
- `DeleteArtifact` leaves document links as stubs and removes card links; lint
  then reports `missing_artifact`.
- `link --doc` and `unlink --doc` write the frontmatter and survive a reread;
  repeating either is a no-op; both accept an id or a name.
- `unlink --card` removes the card link.
- The knowledge list endpoints return `artifacts` with a `url` for resolved
  entries and `missing: true` for stubs, and omit the field when there are none.
- The serving endpoint:
  - returns the bytes with the right `Content-Type` for each row of the serving
    table;
  - sends `text/html` content as `text/plain`;
  - sends `sandbox` on SVG and text and not on PDF;
  - sends `nosniff` on every response and `attachment` on archives;
  - answers a Range request with 206 and the requested bytes, for media and for
    a text artifact — the frontend caps text previews at 256 KB with Range, and
    the 206 must still carry `text/plain; charset=utf-8`;
  - returns 404 for an unknown name, an ambiguous name, a registered artifact
    whose file is gone, and a row whose stored path lies outside the project's
    artifact directory;
  - never serves an unregistered file placed in the directory by hand.
- A filename containing a quote or a newline cannot break the
  `Content-Disposition` header.
