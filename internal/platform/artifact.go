package platform

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"uuid"

	"github.com/jmoiron/sqlx"
)

const artifactSizeLimit = 64 << 20

type Artifact struct {
	ID          uuid.UUID `db:"id" json:"id"`
	ProjectID   uuid.UUID `db:"project_id" json:"-"`
	OwnerID     uuid.UUID `db:"owner_id" json:"-"`
	Owner       string    `db:"owner" json:"owner"`
	Scope       string    `db:"scope" json:"scope"`
	Name        string    `db:"name" json:"name"`
	Kind        string    `db:"kind" json:"kind"`
	MIME        string    `db:"mime" json:"mime"`
	Size        int64     `db:"size" json:"size"`
	ContentHash string    `db:"content_hash" json:"content_hash"`
	CreatedBy   string    `db:"created_by" json:"created_by"`
	CreatedAt   time.Time `db:"created_at" json:"created_at"`
	UpdatedAt   time.Time `db:"updated_at" json:"updated_at"`
	Content     []byte    `db:"-" json:"content,omitempty"`
}

type NewArtifact struct {
	Name    string
	Content []byte
}

type ArtifactTarget struct {
	Card  string
	Entry string
}

func (a Artifact) target() Target {
	return Target{OwnerID: a.OwnerID, Scope: a.Scope}
}

const artifactSelect = `SELECT a.id, a.project_id, a.owner_id, m.handle AS owner, a.scope, a.name, a.kind, a.mime,
	a.size, a.content_hash, a.created_by, a.created_at, a.updated_at
	FROM artifact a JOIN member m ON m.id = a.owner_id`

var artifactNameRe = regexp.MustCompile(`[^a-z0-9]+`)

func errArtifactNotFound(ref string) *Error {
	return ErrNotFound("artifact_not_found", "no artifact "+ref+" in this project", "artifact list")
}

func artifactName(raw string) string {
	base := filepath.Base(strings.TrimSpace(raw))
	ext := strings.ToLower(filepath.Ext(base))
	stem := strings.Trim(artifactNameRe.ReplaceAllString(strings.ToLower(strings.TrimSuffix(base, filepath.Ext(base))), "-"), "-")
	ext = "." + strings.Trim(artifactNameRe.ReplaceAllString(ext, "-"), "-")
	if ext == "." {
		ext = ""
	}
	if stem == "" {
		stem = "artifact"
	}
	return stem + ext
}

func artifactKind(mimeType string) string {
	switch {
	case strings.HasPrefix(mimeType, "image/"):
		return "image"
	case strings.HasPrefix(mimeType, "text/"):
		return "text"
	case strings.HasPrefix(mimeType, "audio/"):
		return "audio"
	case strings.HasPrefix(mimeType, "video/"):
		return "video"
	case mimeType == "application/pdf":
		return "document"
	case mimeType == "application/zip", mimeType == "application/gzip", mimeType == "application/x-tar":
		return "archive"
	default:
		return ""
	}
}

func sniffArtifact(name string, content []byte) string {
	mimeType := http.DetectContentType(content)
	if ext := mime.TypeByExtension(filepath.Ext(name)); mimeType == "application/octet-stream" && ext != "" && ext != "application/pdf" {
		mimeType = ext
	}
	return mimeType
}

func artifactEvent(tx *sqlx.Tx, pr Principal, proj projectAccess, artifactID uuid.UUID, kind string, data map[string]any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO event (org_id, project_id, artifact_id, kind, actor, member_id, data)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		proj.OrgID, proj.ID, artifactID, kind, pr.Actor(), pr.MemberID, raw)
	return err
}

func findArtifact(tx *sqlx.Tx, pr Principal, proj projectAccess, ref string, op Op, deleted bool) (Artifact, error) {
	ref = strings.TrimSpace(ref)
	q := artifactSelect + ` WHERE a.project_id = $1 AND (a.deleted_at IS NOT NULL) = $2`
	args := []any{proj.ID, deleted}
	if id, err := uuid.Parse(ref); err == nil {
		q += ` AND a.id = $3`
		args = append(args, id)
	} else {
		q += ` AND a.name = $3 AND (a.scope = 'project' OR a.owner_id = $4 OR $5)
			ORDER BY (a.owner_id = $4) DESC, a.updated_at DESC LIMIT 1`
		args = append(args, ref, pr.MemberID, pr.IsSystem())
	}
	if !deleted {
		q += ` FOR UPDATE OF a`
	}
	var a Artifact
	err := tx.Get(&a, q, args...)
	if errors.Is(err, sql.ErrNoRows) {
		return a, errArtifactNotFound(ref)
	}
	if err != nil {
		return a, err
	}
	return a, proj.decide(pr, a.target(), op, errArtifactNotFound(ref))
}

func getArtifact(tx *sqlx.Tx, id uuid.UUID) (Artifact, error) {
	var a Artifact
	err := tx.Get(&a, artifactSelect+` WHERE a.id = $1`, id)
	return a, err
}

func freeArtifactName(tx *sqlx.Tx, proj projectAccess, ownerID uuid.UUID, name string) (string, error) {
	stem, ext := strings.TrimSuffix(name, filepath.Ext(name)), filepath.Ext(name)
	candidate := name
	for n := 2; ; n++ {
		var taken bool
		if err := tx.Get(&taken, `SELECT EXISTS (SELECT 1 FROM artifact WHERE project_id = $1 AND name = $2
			AND deleted_at IS NULL AND (scope = 'project' OR owner_id = $3))`, proj.ID, candidate, ownerID); err != nil {
			return "", err
		}
		if !taken {
			return candidate, nil
		}
		candidate = fmt.Sprintf("%s-%d%s", stem, n, ext)
	}
}

func (p *Platform) AddArtifact(ctx context.Context, pr Principal, project string, in NewArtifact) (Artifact, error) {
	var out Artifact
	err := p.Tx(ctx, func(tx *sqlx.Tx) error {
		proj, err := enterProject(tx, pr, project, OpWrite)
		if err != nil {
			return err
		}
		switch size := len(in.Content); {
		case size > artifactSizeLimit:
			return ErrUsage("artifact_too_large",
				fmt.Sprintf("an artifact is at most 64 MiB (%d bytes); this one is %d bytes", artifactSizeLimit, size),
				"add a smaller file, or split it")
		case size == 0:
			return ErrUsage("artifact_empty", "an artifact must not be empty", "artifact add with a file that has content")
		}
		name := artifactName(in.Name)
		mimeType := sniffArtifact(name, in.Content)
		kind := artifactKind(mimeType)
		if kind == "" {
			return ErrUsage("artifact_type_not_allowed", "only images, PDFs, text, audio, video, and archives are accepted",
				"artifact add with another file")
		}
		if name, err = freeArtifactName(tx, proj, pr.MemberID, name); err != nil {
			return err
		}
		sum := sha256.Sum256(in.Content)
		var id uuid.UUID
		if err := tx.Get(&id, `INSERT INTO artifact (org_id, project_id, owner_id, name, kind, mime, size, content_hash, content, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id`,
			proj.OrgID, proj.ID, pr.MemberID, name, kind, mimeType, len(in.Content), hex.EncodeToString(sum[:]),
			in.Content, pr.Actor()); err != nil {
			return err
		}
		if err := artifactEvent(tx, pr, proj, id, "artifact.added", map[string]any{"name": name}); err != nil {
			return err
		}
		out, err = getArtifact(tx, id)
		return err
	})
	return out, err
}

func (p *Platform) ShowArtifact(ctx context.Context, pr Principal, project, ref string) (Artifact, error) {
	var out Artifact
	err := p.Tx(ctx, func(tx *sqlx.Tx) error {
		proj, err := enterProject(tx, pr, project, OpRead)
		if err != nil {
			return err
		}
		if out, err = findArtifact(tx, pr, proj, ref, OpRead, false); err != nil {
			return err
		}
		return tx.Get(&out.Content, `SELECT content FROM artifact WHERE id = $1`, out.ID)
	})
	return out, err
}

func (p *Platform) ListArtifacts(ctx context.Context, pr Principal, project string) ([]Artifact, error) {
	out := []Artifact{}
	err := p.Tx(ctx, func(tx *sqlx.Tx) error {
		proj, err := enterProject(tx, pr, project, OpRead)
		if err != nil {
			return err
		}
		var all []Artifact
		if err := tx.Select(&all, artifactSelect+` WHERE a.project_id = $1 AND a.deleted_at IS NULL
			ORDER BY a.updated_at DESC, a.name`, proj.ID); err != nil {
			return err
		}
		for _, a := range all {
			if proj.verdict(pr, a.target(), OpRead) == Allow {
				out = append(out, a)
			}
		}
		return nil
	})
	return out, err
}

func cardArtifacts(tx *sqlx.Tx, pr Principal, proj projectAccess, cardID uuid.UUID) ([]Artifact, error) {
	var all []Artifact
	if err := tx.Select(&all, artifactSelect+` JOIN artifact_link l ON l.artifact_id = a.id
		WHERE l.card_id = $1 AND a.deleted_at IS NULL ORDER BY l.created_at, a.name`, cardID); err != nil {
		return nil, err
	}
	var out []Artifact
	for _, a := range all {
		if proj.verdict(pr, a.target(), OpRead) == Allow {
			out = append(out, a)
		}
	}
	return out, nil
}

func (p *Platform) ShareArtifact(ctx context.Context, pr Principal, project, ref string) (Artifact, error) {
	var out Artifact
	err := p.Tx(ctx, func(tx *sqlx.Tx) error {
		proj, err := enterProject(tx, pr, project, OpWrite)
		if err != nil {
			return err
		}
		a, err := findArtifact(tx, pr, proj, ref, OpOwn, false)
		if err != nil {
			return err
		}
		if a.Scope != ScopeProject {
			_, err := tx.Exec(`UPDATE artifact SET scope = 'project', updated_at = now() WHERE id = $1`, a.ID)
			if isUniqueViolation(err) {
				return ErrConflict("artifact_name_taken", "the project already has an artifact named "+a.Name, "artifact list")
			}
			if err != nil {
				return err
			}
			if err := artifactEvent(tx, pr, proj, a.ID, "artifact.shared", map[string]any{"name": a.Name}); err != nil {
				return err
			}
		}
		out, err = getArtifact(tx, a.ID)
		return err
	})
	return out, err
}

func (p *Platform) LinkArtifact(ctx context.Context, pr Principal, project, ref string, to ArtifactTarget) error {
	return p.Tx(ctx, func(tx *sqlx.Tx) error {
		proj, err := enterProject(tx, pr, project, OpWrite)
		if err != nil {
			return err
		}
		a, err := findArtifact(tx, pr, proj, ref, OpRead, false)
		if err != nil {
			return err
		}
		var cardID, entryID *uuid.UUID
		var data map[string]any
		switch {
		case to.Card != "" && to.Entry == "":
			c, err := loadCard(tx, pr, proj, to.Card, OpWrite, false)
			if err != nil {
				return err
			}
			cardID, data = &c.ID, map[string]any{"card": c.Ref}
		case to.Entry != "" && to.Card == "":
			e, err := findEntry(tx, pr, proj, to.Entry, OpRead, false, false)
			if err != nil {
				return err
			}
			entryID, data = &e.ID, map[string]any{"entry": e.Slug}
		default:
			return ErrUsage("link_target_required", "a link needs one card or one entry", "artifact link with a card or an entry")
		}
		if _, err := tx.Exec(`INSERT INTO artifact_link (artifact_id, card_id, entry_id, member_id, actor)
			VALUES ($1, $2, $3, $4, $5) ON CONFLICT DO NOTHING`, a.ID, cardID, entryID, pr.MemberID, pr.Actor()); err != nil {
			return err
		}
		return artifactEvent(tx, pr, proj, a.ID, "artifact.linked", data)
	})
}

func (p *Platform) DeleteArtifact(ctx context.Context, pr Principal, project, ref string) error {
	return p.Tx(ctx, func(tx *sqlx.Tx) error {
		proj, err := enterProject(tx, pr, project, OpWrite)
		if err != nil {
			return err
		}
		a, err := findArtifact(tx, pr, proj, ref, OpWrite, false)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE artifact SET deleted_at = now(), deleted_by = $2, updated_at = now() WHERE id = $1`,
			a.ID, pr.Actor()); err != nil {
			return err
		}
		return artifactEvent(tx, pr, proj, a.ID, "artifact.deleted", map[string]any{"name": a.Name})
	})
}

func (p *Platform) RestoreArtifact(ctx context.Context, pr Principal, project, ref string) (Artifact, error) {
	var out Artifact
	err := p.Tx(ctx, func(tx *sqlx.Tx) error {
		proj, err := enterProject(tx, pr, project, OpWrite)
		if err != nil {
			return err
		}
		a, err := findArtifact(tx, pr, proj, ref, OpWrite, true)
		if err != nil {
			return err
		}
		_, err = tx.Exec(`UPDATE artifact SET deleted_at = NULL, deleted_by = NULL, updated_at = now() WHERE id = $1`, a.ID)
		if isUniqueViolation(err) {
			return ErrConflict("artifact_name_taken", "another artifact is now named "+a.Name, "artifact list")
		}
		if err != nil {
			return err
		}
		if err := artifactEvent(tx, pr, proj, a.ID, "artifact.restored", map[string]any{"name": a.Name}); err != nil {
			return err
		}
		out, err = getArtifact(tx, a.ID)
		return err
	})
	return out, err
}
