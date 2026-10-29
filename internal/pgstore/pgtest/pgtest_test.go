package pgtest

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

const childEnv = "PGTEST_NO_RUNTIME_CHILD"

func TestPlat_PLAT_C4_NoContainerRuntimeFailsNamingDocker(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestNoRuntimeChild$", "-test.v", "-test.count=1")
	cmd.Env = append(os.Environ(),
		childEnv+"=1",
		"DOCKER_HOST=unix:///nonexistent/trellis-pgtest/docker.sock",
		"TESTCONTAINERS_RYUK_DISABLED=true",
	)
	out, err := cmd.CombinedOutput()
	text := string(out)
	if err == nil {
		t.Fatalf("the child test passed without a container runtime:\n%s", text)
	}
	if !strings.Contains(text, "--- FAIL: TestNoRuntimeChild") {
		t.Fatalf("the child test is not reported as failed:\n%s", text)
	}
	if strings.Contains(text, "--- SKIP") {
		t.Fatalf("the child test is reported as skipped:\n%s", text)
	}
	if !strings.Contains(text, "Docker") {
		t.Fatalf("the failure does not name Docker:\n%s", text)
	}
}

func TestNoRuntimeChild(t *testing.T) {
	if os.Getenv(childEnv) == "" {
		return
	}
	URL(t)
}

func TestURLGivesAnEmptyDatabase(t *testing.T) {
	if os.Getenv(childEnv) != "" {
		return
	}
	db := Open(t)
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM information_schema.tables WHERE table_schema = 'public'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("a fresh database has %d tables", n)
	}
}
