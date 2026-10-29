package mcpserver

import (
	"context"
	"slices"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/mtch3n/trellis/internal/platform"
)

type VaultListIn struct {
	Project    string `json:"project,omitempty" jsonschema:"the project key; defaults to the X-Trellis-Project header"`
	Unverified bool   `json:"unverified,omitempty" jsonschema:"only global entries whose verify_by has passed"`
}

type VaultListOut struct {
	Entries []platform.Entry `json:"entries"`
}

type VaultSearchIn struct {
	Project string `json:"project,omitempty" jsonschema:"the project key; defaults to the X-Trellis-Project header"`
	Query   string `json:"query" jsonschema:"the words to search for"`
	Method  string `json:"method,omitempty" jsonschema:"fts, vector or hybrid; default hybrid"`
	Label   string `json:"label,omitempty" jsonschema:"only entries with this label"`
	Limit   int    `json:"limit,omitempty" jsonschema:"the most hits to return; default 50"`
}

type VaultSearchOut struct {
	Hits []platform.EntryHit `json:"hits"`
}

type VaultShowIn struct {
	Project string `json:"project,omitempty" jsonschema:"the project key; defaults to the X-Trellis-Project header"`
	Slug    string `json:"slug" jsonschema:"the entry slug or id"`
}

type VaultWriteIn struct {
	Project    string            `json:"project,omitempty" jsonschema:"the project key; defaults to the X-Trellis-Project header"`
	Action     string            `json:"action" jsonschema:"new, edit, move, delete, restore, link or unlink"`
	Slug       string            `json:"slug,omitempty" jsonschema:"the entry slug or id; for new, the slug to give it (default: from the title)"`
	Scope      string            `json:"scope,omitempty" jsonschema:"personal (the default); project and global come only through vault_share"`
	Title      *string           `json:"title,omitempty" jsonschema:"the title (new, edit)"`
	Summary    *string           `json:"summary,omitempty" jsonschema:"the summary (new, edit)"`
	Body       *string           `json:"body,omitempty" jsonschema:"the markdown body (new, edit)"`
	Template   string            `json:"template,omitempty" jsonschema:"the template to fill (new)"`
	Provenance string            `json:"provenance,omitempty" jsonschema:"authored, prompted or extracted (new)"`
	Private    *bool             `json:"private,omitempty" jsonschema:"keep the entry out of recall, pins and the brief (new, edit)"`
	Tags       *[]string         `json:"tags,omitempty" jsonschema:"the tags (new, edit)"`
	Labels     *[]string         `json:"labels,omitempty" jsonschema:"the labels (new, edit)"`
	Sources    *[]string         `json:"sources,omitempty" jsonschema:"the cited entries, cards or URLs (new, edit)"`
	Set        map[string]string `json:"set,omitempty" jsonschema:"template fields to set; an empty value removes one (new, edit)"`
	To         string            `json:"to,omitempty" jsonschema:"the new slug (move)"`
	Card       string            `json:"card,omitempty" jsonschema:"the card ref (link, unlink)"`
	IfVersion  int64             `json:"if_version,omitempty" jsonschema:"the version you read (edit, move)"`
}

type VaultWriteOut struct {
	Entry *platform.Entry `json:"entry,omitempty"`
}

type VaultShareIn struct {
	Project string `json:"project,omitempty" jsonschema:"the project key; defaults to the X-Trellis-Project header"`
	Action  string `json:"action" jsonschema:"publish, demote, nominate, promote, verify, pin or unpin"`
	Slug    string `json:"slug" jsonschema:"the entry slug or id"`
	Reason  string `json:"reason,omitempty" jsonschema:"why (nominate, promote)"`
	Recap   string `json:"recap,omitempty" jsonschema:"the line the brief shows; default: the summary (pin)"`
}

type VaultShareOut struct {
	Result *platform.ShareResult `json:"result,omitempty"`
	Pin    *platform.Pin         `json:"pin,omitempty"`
}

type VaultCheckIn struct {
	Project string `json:"project,omitempty" jsonschema:"the project key; defaults to the X-Trellis-Project header"`
	Action  string `json:"action" jsonschema:"lint, health, history, diff or nominations"`
	Slug    string `json:"slug,omitempty" jsonschema:"the entry slug or id (history, diff)"`
	From    int64  `json:"from,omitempty" jsonschema:"the older version (diff); default: the one before to"`
	To      int64  `json:"to,omitempty" jsonschema:"the newer version (diff); default: the latest"`
}

type VaultCheckOut struct {
	Diagnostics []platform.Diagnostic  `json:"diagnostics,omitempty"`
	Health      []platform.HealthLine  `json:"health,omitempty"`
	Revisions   []platform.Revision    `json:"revisions,omitempty"`
	Diff        *platform.RevisionDiff `json:"diff,omitempty"`
	Nominations []platform.Nomination  `json:"nominations,omitempty"`
}

type RecallIn struct {
	Project string `json:"project,omitempty" jsonschema:"the project key; defaults to the X-Trellis-Project header"`
	Text    string `json:"text" jsonschema:"the prompt text to recall entries for"`
	Limit   int    `json:"limit,omitempty" jsonschema:"the most hits to return; default 5"`
}

type RecallOut struct {
	Hits []platform.RecallHit `json:"hits"`
}

type ArtifactIn struct {
	Project string `json:"project,omitempty" jsonschema:"the project key; defaults to the X-Trellis-Project header"`
	Action  string `json:"action" jsonschema:"add, show, list, share, link, delete or restore"`
	Ref     string `json:"ref,omitempty" jsonschema:"the artifact name or id (show, share, link, delete, restore)"`
	Name    string `json:"name,omitempty" jsonschema:"the file name (add)"`
	Content []byte `json:"content,omitempty" jsonschema:"the file bytes, base64 (add)"`
	Card    string `json:"card,omitempty" jsonschema:"the card ref to link to (link)"`
	Entry   string `json:"entry,omitempty" jsonschema:"the entry slug to link to (link)"`
}

type ArtifactOut struct {
	Artifact  *platform.Artifact  `json:"artifact,omitempty"`
	Artifacts []platform.Artifact `json:"artifacts,omitempty"`
}

type TemplateIn struct {
	Project   string `json:"project,omitempty" jsonschema:"the project key; defaults to the X-Trellis-Project header"`
	Action    string `json:"action" jsonschema:"list, show, new or edit"`
	Name      string `json:"name,omitempty" jsonschema:"the template name (show, new, edit)"`
	Raw       string `json:"raw,omitempty" jsonschema:"the whole template file, header and body (new, edit)"`
	IfVersion int64  `json:"if_version,omitempty" jsonschema:"the version you read (edit)"`
}

type TemplateOut struct {
	Template  *platform.Template  `json:"template,omitempty"`
	Templates []platform.Template `json:"templates,omitempty"`
}

var (
	vaultWriteActions = []string{"new", "edit", "move", "delete", "restore", "link", "unlink"}
	vaultShareActions = []string{"publish", "demote", "nominate", "promote", "verify", "pin", "unpin"}
	vaultCheckActions = []string{"lint", "health", "history", "diff", "nominations"}
	artifactActions   = []string{"add", "show", "list", "share", "link", "delete", "restore"}
	templateActions   = []string{"list", "show", "new", "edit"}
)

func (t *tools) registerVault(s *mcp.Server) {
	t.add(s, &mcp.Tool{Name: "vault_list", Description: "List the vault entries you see in a project", Annotations: readOnly}, nil, t.vaultList)
	t.add(s, &mcp.Tool{Name: "vault_search", Description: "Search the vault entries you see by full text, vector or both", Annotations: readOnly}, nil, t.vaultSearch)
	t.add(s, &mcp.Tool{Name: "vault_show", Description: "Show one vault entry with its body and links", Annotations: readOnly}, nil, t.vaultShow)
	t.add(s, &mcp.Tool{Name: "vault_write", Description: "Create, edit, move, delete, restore an entry, or link it to a card"}, vaultWriteActions, t.vaultWrite)
	t.add(s, &mcp.Tool{Name: "vault_share", Description: "Publish an entry through the gate, demote, nominate, promote, verify, pin or unpin it"}, vaultShareActions, t.vaultShare)
	t.add(s, &mcp.Tool{Name: "vault_check", Description: "Lint the vault, count its health, list an entry's history or diff, list nominations", Annotations: readOnly}, vaultCheckActions, t.vaultCheck)
	t.add(s, &mcp.Tool{Name: "recall", Description: "Recall the entries that bear on a prompt", Annotations: readOnly}, nil, t.recall)
	t.add(s, &mcp.Tool{Name: "artifact", Description: "Add, show, list, share, link, delete or restore a file artifact"}, artifactActions, t.artifact)
	t.add(s, &mcp.Tool{Name: "template", Description: "List, show, create or edit an entry template"}, templateActions, t.template)
}

func (t *tools) vaultList(ctx context.Context, c call, in VaultListIn) (VaultListOut, error) {
	project, err := c.project(in.Project)
	if err != nil {
		return VaultListOut{}, err
	}
	list := t.p.ListEntries
	if in.Unverified {
		list = t.p.UnverifiedEntries
	}
	entries, err := list(ctx, c.pr, project)
	return VaultListOut{Entries: entries}, err
}

func (t *tools) vaultSearch(ctx context.Context, c call, in VaultSearchIn) (VaultSearchOut, error) {
	if err := needs("vault_search", "search", "query", in.Query); err != nil {
		return VaultSearchOut{}, err
	}
	project, err := c.project(in.Project)
	if err != nil {
		return VaultSearchOut{}, err
	}
	hits, err := t.p.Search(ctx, c.pr, project, in.Query, platform.SearchOpts{Limit: in.Limit, Method: in.Method, Label: in.Label})
	return VaultSearchOut{Hits: hits}, err
}

func (t *tools) vaultShow(ctx context.Context, c call, in VaultShowIn) (platform.Entry, error) {
	if err := needs("vault_show", "show", "slug", in.Slug); err != nil {
		return platform.Entry{}, err
	}
	project, err := c.project(in.Project)
	if err != nil {
		return platform.Entry{}, err
	}
	return t.p.ShowEntry(ctx, c.pr, project, in.Slug)
}

func (t *tools) vaultWrite(ctx context.Context, c call, in VaultWriteIn) (VaultWriteOut, error) {
	const tool = "vault_write"
	var err error
	switch in.Action {
	case "new":
		err = needs(tool, in.Action, "title", deref(in.Title))
	case "edit", "delete", "restore":
		err = needs(tool, in.Action, "slug", in.Slug)
	case "move":
		err = cmpErr(needs(tool, in.Action, "slug", in.Slug), needs(tool, in.Action, "to", in.To))
	case "link", "unlink":
		err = cmpErr(needs(tool, in.Action, "slug", in.Slug), needs(tool, in.Action, "card", in.Card))
	default:
		err = badAction(tool, in.Action, vaultWriteActions)
	}
	if err != nil {
		return VaultWriteOut{}, err
	}
	project, err := c.project(in.Project)
	if err != nil {
		return VaultWriteOut{}, err
	}
	var e platform.Entry
	switch in.Action {
	case "new":
		e, err = t.p.CreateEntry(ctx, c.pr, project, platform.NewEntry{
			Scope: in.Scope, Slug: in.Slug, Title: deref(in.Title), Summary: deref(in.Summary), Body: deref(in.Body),
			Template: in.Template, Provenance: in.Provenance, Private: in.Private != nil && *in.Private,
			Tags: derefList(in.Tags), Labels: derefList(in.Labels), Sources: derefList(in.Sources), Set: in.Set,
		})
	case "edit":
		var scope *string
		if in.Scope != "" {
			scope = &in.Scope
		}
		e, err = t.p.EditEntry(ctx, c.pr, project, in.Slug, platform.EntryEdit{
			Scope: scope, Title: in.Title, Summary: in.Summary, Body: in.Body, Private: in.Private,
			Tags: in.Tags, Labels: in.Labels, Sources: in.Sources, Set: in.Set, IfVersion: in.IfVersion,
		})
	case "move":
		e, err = t.p.MoveEntry(ctx, c.pr, project, in.Slug, in.To, in.IfVersion)
	case "delete":
		return VaultWriteOut{}, t.p.DeleteEntry(ctx, c.pr, project, in.Slug)
	case "restore":
		e, err = t.p.RestoreEntry(ctx, c.pr, project, in.Slug)
	case "link", "unlink":
		change := t.p.LinkEntry
		if in.Action == "unlink" {
			change = t.p.UnlinkEntry
		}
		if err := change(ctx, c.pr, project, in.Slug, in.Card); err != nil {
			return VaultWriteOut{}, err
		}
		e, err = t.p.ShowEntry(ctx, c.pr, project, in.Slug)
	}
	if err != nil {
		return VaultWriteOut{}, err
	}
	return VaultWriteOut{Entry: &e}, nil
}

func (t *tools) vaultShare(ctx context.Context, c call, in VaultShareIn) (VaultShareOut, error) {
	const tool = "vault_share"
	if !slices.Contains(vaultShareActions, in.Action) {
		return VaultShareOut{}, badAction(tool, in.Action, vaultShareActions)
	}
	if err := needs(tool, in.Action, "slug", in.Slug); err != nil {
		return VaultShareOut{}, err
	}
	project, err := c.project(in.Project)
	if err != nil {
		return VaultShareOut{}, err
	}
	var r platform.ShareResult
	switch in.Action {
	case "publish":
		r, err = t.p.Publish(ctx, c.pr, project, in.Slug)
	case "demote":
		r, err = t.p.Demote(ctx, c.pr, project, in.Slug)
	case "promote":
		r, err = t.p.Promote(ctx, c.pr, project, in.Slug, in.Reason)
	case "verify":
		r, err = t.p.Verify(ctx, c.pr, project, in.Slug)
	case "nominate":
		return VaultShareOut{}, t.p.Nominate(ctx, c.pr, project, in.Slug, in.Reason)
	case "unpin":
		return VaultShareOut{}, t.p.UnpinEntry(ctx, c.pr, project, in.Slug)
	case "pin":
		pin, err := t.p.PinEntry(ctx, c.pr, project, in.Slug, in.Recap)
		if err != nil {
			return VaultShareOut{}, err
		}
		return VaultShareOut{Pin: &pin}, nil
	}
	if err != nil {
		return VaultShareOut{}, err
	}
	return VaultShareOut{Result: &r}, nil
}

func (t *tools) vaultCheck(ctx context.Context, c call, in VaultCheckIn) (VaultCheckOut, error) {
	const tool = "vault_check"
	var err error
	switch in.Action {
	case "lint", "health", "nominations":
	case "history", "diff":
		err = needs(tool, in.Action, "slug", in.Slug)
	default:
		err = badAction(tool, in.Action, vaultCheckActions)
	}
	if err != nil {
		return VaultCheckOut{}, err
	}
	project, err := c.project(in.Project)
	if err != nil {
		return VaultCheckOut{}, err
	}
	var out VaultCheckOut
	switch in.Action {
	case "lint":
		out.Diagnostics, err = t.p.Lint(ctx, c.pr, project)
	case "health":
		out.Health, err = t.p.Health(ctx, c.pr, project)
	case "nominations":
		out.Nominations, err = t.p.Nominations(ctx, c.pr, project)
	case "history":
		out.Revisions, err = t.p.EntryHistory(ctx, c.pr, project, in.Slug)
	case "diff":
		var d platform.RevisionDiff
		d, err = t.p.DiffEntry(ctx, c.pr, project, in.Slug, in.From, in.To)
		out.Diff = &d
	}
	if err != nil {
		return VaultCheckOut{}, err
	}
	return out, nil
}

func (t *tools) recall(ctx context.Context, c call, in RecallIn) (RecallOut, error) {
	project, err := c.project(in.Project)
	if err != nil {
		return RecallOut{}, err
	}
	hits, err := t.p.Recall(ctx, c.pr, project, in.Text, platform.RecallOpts{Limit: in.Limit})
	return RecallOut{Hits: hits}, err
}

func (t *tools) artifact(ctx context.Context, c call, in ArtifactIn) (ArtifactOut, error) {
	const tool = "artifact"
	var err error
	switch in.Action {
	case "add":
		err = needs(tool, in.Action, "name", in.Name)
		if err == nil && len(in.Content) == 0 {
			err = needs(tool, in.Action, "content", "")
		}
	case "show", "share", "link", "delete", "restore":
		err = needs(tool, in.Action, "ref", in.Ref)
	case "list":
	default:
		err = badAction(tool, in.Action, artifactActions)
	}
	if err != nil {
		return ArtifactOut{}, err
	}
	project, err := c.project(in.Project)
	if err != nil {
		return ArtifactOut{}, err
	}
	var a platform.Artifact
	switch in.Action {
	case "add":
		a, err = t.p.AddArtifact(ctx, c.pr, project, platform.NewArtifact{Name: in.Name, Content: in.Content})
	case "show":
		a, err = t.p.ShowArtifact(ctx, c.pr, project, in.Ref)
	case "share":
		a, err = t.p.ShareArtifact(ctx, c.pr, project, in.Ref)
	case "restore":
		a, err = t.p.RestoreArtifact(ctx, c.pr, project, in.Ref)
	case "list":
		list, err := t.p.ListArtifacts(ctx, c.pr, project)
		return ArtifactOut{Artifacts: list}, err
	case "delete":
		return ArtifactOut{}, t.p.DeleteArtifact(ctx, c.pr, project, in.Ref)
	case "link":
		if err := t.p.LinkArtifact(ctx, c.pr, project, in.Ref, platform.ArtifactTarget{Card: in.Card, Entry: in.Entry}); err != nil {
			return ArtifactOut{}, err
		}
		a, err = t.p.ShowArtifact(ctx, c.pr, project, in.Ref)
	}
	if err != nil {
		return ArtifactOut{}, err
	}
	return ArtifactOut{Artifact: &a}, nil
}

func (t *tools) template(ctx context.Context, c call, in TemplateIn) (TemplateOut, error) {
	const tool = "template"
	var err error
	switch in.Action {
	case "list":
	case "show":
		err = needs(tool, in.Action, "name", in.Name)
	case "new", "edit":
		err = cmpErr(needs(tool, in.Action, "name", in.Name), needs(tool, in.Action, "raw", in.Raw))
	default:
		err = badAction(tool, in.Action, templateActions)
	}
	if err != nil {
		return TemplateOut{}, err
	}
	project, err := c.project(in.Project)
	if err != nil {
		return TemplateOut{}, err
	}
	var tm platform.Template
	switch in.Action {
	case "list":
		list, err := t.p.ListTemplates(ctx, c.pr, project)
		return TemplateOut{Templates: list}, err
	case "show":
		tm, err = t.p.ShowTemplate(ctx, c.pr, project, in.Name)
	case "new":
		tm, err = t.p.NewTemplate(ctx, c.pr, project, in.Name, in.Raw)
	case "edit":
		tm, err = t.p.EditTemplate(ctx, c.pr, project, in.Name, in.Raw, in.IfVersion)
	}
	if err != nil {
		return TemplateOut{}, err
	}
	return TemplateOut{Template: &tm}, nil
}

func derefList(l *[]string) []string {
	if l == nil {
		return nil
	}
	return *l
}
