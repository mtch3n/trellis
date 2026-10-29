package platform

import (
	"slices"
	"strings"
	"sync"
	"testing"
)

const awsKeyBody = "Deploy notes\n\naws_access_key_id = AKIAZ3MSJV4WWNKPQX7B\n"

func TestVault_VAULT_C30_CleanEntryPublishesWithGateLabel(t *testing.T) {
	w := newWorld(t)
	ctx := t.Context()
	a, b := w.join("ann"), w.join("bob")
	id := w.gateSeedEntry(a, gateSeed{slug: "deploy-steps", body: "Run make deploy from the repo root."})

	got := w.must(w.p.Publish(ctx, a, "APP", "deploy-steps"))
	if got.Scope != "project" || !slices.Contains(got.Labels, "gate") {
		t.Fatalf("published = %+v", got)
	}
	row := w.entryRow(id)
	if row.Scope != "project" || row.Labels != "gate" {
		t.Fatalf("row = %+v", row)
	}
	if err := w.p.Nominate(ctx, b, "APP", "deploy-steps", "every repo needs this"); err != nil {
		t.Fatalf("other member cannot see the published entry: %v", err)
	}
	var checks []string
	if err := w.db.Select(&checks, `SELECT jsonb_array_elements_text(data->'checks') FROM event
		WHERE entry_id = $1 AND kind = 'entry.published'`, id); err != nil {
		t.Fatal(err)
	}
	want := []string{"private", "secret", "template", "sources", "duplicate"}
	if !slices.Equal(checks, want) {
		t.Fatalf("event checks = %v, want %v", checks, want)
	}
}

func TestVault_VAULT_C31_SecretFailsGateWithLine(t *testing.T) {
	w := newWorld(t)
	a := w.join("ann")
	id := w.gateSeedEntry(a, gateSeed{slug: "aws-setup", body: awsKeyBody})
	before := w.entryRow(id)

	_, err := w.p.Publish(t.Context(), a, "APP", "aws-setup")
	checks := gateChecks(t, err)
	if len(checks) != 1 || checks[0].Check != "secret" || !strings.Contains(checks[0].Problem, "line 3") || checks[0].Fix == "" {
		t.Fatalf("checks = %+v", checks)
	}
	if strings.Contains(err.Error(), "AKIAZ3MSJV4WWNKPQX7B") {
		t.Fatalf("the answer repeats the secret: %v", err)
	}
	if after := w.entryRow(id); after != before {
		t.Fatalf("entry changed: %+v -> %+v", before, after)
	}
}

func TestVault_VAULT_C32_GateListsEveryFailedCheckWithFix(t *testing.T) {
	w := newWorld(t)
	a := w.join("ann")
	w.exec(`INSERT INTO vault_template (project_id, name, raw) VALUES ($1, 'decision', $2)`, w.project,
		"---\nenforce: reject\nrequired: [team]\n---\n# {{title}}\n")
	id := w.gateSeedEntry(a, gateSeed{slug: "pick-db", body: "We pick Postgres.", template: "decision",
		fields: map[string]any{"team": ""}, sources: []string{"/APP/cards/APP-99", "https://example.com/doc"}})

	_, err := w.p.Publish(t.Context(), a, "APP", "pick-db")
	checks := gateChecks(t, err)
	names := []string{}
	for _, c := range checks {
		names = append(names, c.Check)
		if c.Fix == "" {
			t.Fatalf("check %s has no fix", c.Check)
		}
	}
	if !slices.Equal(names, []string{"template", "sources"}) {
		t.Fatalf("checks = %+v", checks)
	}
	if !strings.Contains(checks[0].Problem, "team") || !strings.Contains(checks[1].Problem, "APP-99") {
		t.Fatalf("checks = %+v", checks)
	}
	if w.entryRow(id).Scope != "personal" {
		t.Fatal("entry moved")
	}
}

func TestVault_VAULT_C33_PrivateEntryIsNeitherPublishedNorPromoted(t *testing.T) {
	w := newWorld(t)
	ctx := t.Context()
	a := w.join("ann")
	personal := w.gateSeedEntry(a, gateSeed{slug: "secret-plan", body: "only mine", private: true})
	before := w.entryRow(personal)
	_, err := w.p.Publish(ctx, a, "APP", "secret-plan")
	checks := gateChecks(t, err)
	if len(checks) != 1 || checks[0].Check != "private" {
		t.Fatalf("checks = %+v", checks)
	}
	if w.entryRow(personal) != before {
		t.Fatal("personal entry changed")
	}

	project := w.gateSeedEntry(a, gateSeed{slug: "made-private", body: "shared then hidden", scope: "project", private: true})
	before = w.entryRow(project)
	_, err = w.p.Promote(ctx, a, "APP", "made-private", "useful everywhere")
	wantCode(t, err, "private")
	if w.entryRow(project) != before {
		t.Fatal("project entry changed")
	}
}

func TestVault_VAULT_C35_BodyDifferingInCaseAndSpaceIsDuplicate(t *testing.T) {
	w := newWorld(t)
	a, b := w.join("ann"), w.join("bob")
	w.gateSeedEntry(a, gateSeed{slug: "retry-rule", body: "Retry the Deploy  three times,\n\tthen page.", scope: "project"})
	id := w.gateSeedEntry(b, gateSeed{slug: "retry-again", body: "retry the deploy three times, then page."})

	_, err := w.p.Publish(t.Context(), b, "APP", "retry-again")
	checks := gateChecks(t, err)
	if len(checks) != 1 || checks[0].Check != "duplicate" || !strings.Contains(checks[0].Problem, "retry-rule") {
		t.Fatalf("checks = %+v", checks)
	}
	if w.entryRow(id).Scope != "personal" {
		t.Fatal("entry moved")
	}
}

func TestVault_VAULT_C36_NearDuplicatePasses(t *testing.T) {
	w := newWorld(t)
	a, b := w.join("ann"), w.join("bob")
	w.gateSeedEntry(a, gateSeed{slug: "retry-rule", body: "Retry the deploy three times, then page.", scope: "project"})
	w.gateSeedEntry(b, gateSeed{slug: "retry-twice", body: "Retry the deploy two times, then page."})

	got := w.must(w.p.Publish(t.Context(), b, "APP", "retry-twice"))
	if got.Scope != "project" {
		t.Fatalf("published = %+v", got)
	}
}

func TestVault_VAULT_C37_ConcurrentPublishSameSlugOneWins(t *testing.T) {
	w := newWorld(t)
	ctx := t.Context()
	for round := range 5 {
		a, b := w.join("ann"+string(rune('a'+round))), w.join("bob"+string(rune('a'+round)))
		slug := "race-" + string(rune('a'+round))
		w.gateSeedEntry(a, gateSeed{slug: slug, body: "ann's text " + slug})
		w.gateSeedEntry(b, gateSeed{slug: slug, body: "bob's text " + slug})

		errs := make([]error, 2)
		var wg sync.WaitGroup
		for i, pr := range []Principal{a, b} {
			wg.Go(func() {
				_, errs[i] = w.p.Publish(ctx, pr, "APP", slug)
			})
		}
		wg.Wait()

		var failed error
		passed := 0
		for _, err := range errs {
			if err == nil {
				passed++
			} else {
				failed = err
			}
		}
		if passed != 1 {
			t.Fatalf("round %d: errs = %v", round, errs)
		}
		checks := gateChecks(t, failed)
		if len(checks) != 1 || checks[0].Check != "duplicate" || !strings.Contains(checks[0].Problem, slug) {
			t.Fatalf("round %d: checks = %+v", round, checks)
		}
	}
}
