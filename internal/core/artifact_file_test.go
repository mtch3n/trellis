package core

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func fileErrCode(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

func TestArtifactFileResolvesARegisteredArtifact(t *testing.T) {
	c, p, _ := vaultCore(t)
	a := addArtifact(t, c, p.ID, "serve.png", "\x89PNG\r\n\x1a\nx")

	got, path, err := c.ArtifactFile(t.Context(), p.ID, a.Name)
	if err != nil {
		t.Fatalf("ArtifactFile: %v", err)
	}
	want, err := filepath.EvalSymlinks(a.Path)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != a.ID || path != want {
		t.Errorf("got %s at %q, want %s at %q", got.ID, path, a.ID, want)
	}
}

// A project id that does not exist must fail the same way any other refusal
// does, not with a raw sql.ErrNoRows that the web layer would map to 500.
func TestArtifactFileWithUnknownProjectIsNotFound(t *testing.T) {
	c, p, _ := vaultCore(t)
	a := addArtifact(t, c, p.ID, "serve.png", "\x89PNG\r\n\x1a\nx")

	_, _, err := c.ArtifactFile(t.Context(), "no-such-project", a.Name)
	if fileErrCode(err) != "artifact_not_found" {
		t.Errorf("err = %v, want artifact_not_found", err)
	}
}

func TestArtifactFileRefuses(t *testing.T) {
	cases := map[string]func(t *testing.T, c *Core, projectID string, a Artifact) string{
		"an unknown name": func(t *testing.T, c *Core, projectID string, a Artifact) string {
			return "nope.png"
		},
		// A shared name is no longer constructible: UNIQUE(project_id, name)
		// (TRELLIS-36) refuses the very insert that used to simulate it, so
		// ArtifactFile's "more than one match" branch is unreachable now.
		"a registered artifact whose file is gone": func(t *testing.T, c *Core, projectID string, a Artifact) string {
			if err := os.Remove(a.Path); err != nil {
				t.Fatal(err)
			}
			return a.Name
		},
		// Path is derived from name (TRELLIS-36), not stored, so the only way
		// left to make it resolve outside the artifacts directory is a name
		// that escapes it -- a corrupted or hand-edited row, since Slugify
		// never produces one.
		"a name that resolves outside the directory": func(t *testing.T, c *Core, projectID string, a Artifact) string {
			outside := filepath.Join(filepath.Dir(filepath.Dir(a.Path)), "secret.txt")
			if err := os.WriteFile(outside, []byte("not yours"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := c.db.Exec(`UPDATE artifact SET name = ? WHERE id = ?`, "../secret.txt", a.ID); err != nil {
				t.Fatal(err)
			}
			return "../secret.txt"
		},
		"an unregistered file placed in the directory": func(t *testing.T, c *Core, projectID string, a Artifact) string {
			sneaky := filepath.Join(filepath.Dir(a.Path), "sneaky.png")
			if err := os.WriteFile(sneaky, []byte("\x89PNG\r\n\x1a\nx"), 0o600); err != nil {
				t.Fatal(err)
			}
			return "sneaky.png"
		},
		"a symlink leading out of the directory": func(t *testing.T, c *Core, projectID string, a Artifact) string {
			outside := filepath.Join(t.TempDir(), "secret.txt")
			if err := os.WriteFile(outside, []byte("not yours"), 0o600); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(filepath.Dir(a.Path), "link.png")
			if err := os.Symlink(outside, link); err != nil {
				t.Skipf("symlinks unavailable here: %v", err)
			}
			if _, err := c.db.Exec(`UPDATE artifact SET name = 'link.png' WHERE id = ?`, a.ID); err != nil {
				t.Fatal(err)
			}
			return "link.png"
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			c, p, _ := vaultCore(t)
			a := addArtifact(t, c, p.ID, "base.png", "\x89PNG\r\n\x1a\nx")
			ref := setup(t, c, p.ID, a)
			if _, _, err := c.ArtifactFile(t.Context(), p.ID, ref); fileErrCode(err) != "artifact_not_found" {
				t.Errorf("err = %v, want artifact_not_found", err)
			}
		})
	}
}
