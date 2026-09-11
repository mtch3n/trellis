// Package home resolves the trellis storage root.
package home

import (
	"os"
	"path/filepath"
	"runtime"
)

// Root returns the storage root, creating it if it does not exist.
// TRELLIS_HOME wins everywhere; otherwise the platform default applies.
func Root() (string, error) {
	dir := os.Getenv("TRELLIS_HOME")
	if dir == "" {
		var err error
		if dir, err = defaultRoot(); err != nil {
			return "", err
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

func defaultRoot() (string, error) {
	if runtime.GOOS == "windows" {
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			return filepath.Join(local, "trellis"), nil
		}
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, ".trellis"), nil
}

// DBPath returns the path to the SQLite database inside the storage root.
func DBPath() (string, error) {
	root, err := Root()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "trellis.db"), nil
}

// ProjectRoot returns the private storage directory for one project.
func ProjectRoot(projectKey string) (string, error) {
	root, err := Root()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, "projects", projectKey)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// VectorDBPath returns the disposable per-project vector index path.
func VectorDBPath(projectKey string) (string, error) {
	dir, err := ProjectRoot(projectKey)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "vectors.db"), nil
}
