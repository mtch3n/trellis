package cli

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"github.com/mtch3n/trellis/internal/version"
	"github.com/spf13/cobra"
	"golang.org/x/term"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type release struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

func newUpdateCmd() *cobra.Command {
	var check, force bool
	cmd := &cobra.Command{Use: "update", Short: "Check for and install the latest GitHub release", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		// --force answers the prompt in advance: asking again after the user
		// has already said "do it regardless" is noise.
		return checkForUpdate(cmd.Context(), !check, !force, force)
	}}
	cmd.Flags().BoolVar(&check, "check", false, "only check; do not install")
	cmd.Flags().BoolVar(&force, "force", false, "reinstall without prompting, even when already up to date")
	cmd.MarkFlagsMutuallyExclusive("check", "force")
	return cmd
}

func checkForUpdate(ctx context.Context, install, prompt, force bool) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/"+version.Repository+"/releases/latest", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("check latest release: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GitHub latest release returned %s", resp.Status)
	}
	var r release
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return err
	}
	switch {
	case r.TagName != version.Version:
		fmt.Printf("new Trellis release available: %s (current %s)\n", r.TagName, version.Version)
	case force:
		fmt.Printf("trellis %s is up to date; reinstalling\n", version.Version)
	default:
		fmt.Printf("trellis %s is up to date\n", version.Version)
		return nil
	}
	if !install {
		return nil
	}
	if prompt {
		if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
			return fmt.Errorf("an update is available; run trellis update from an interactive terminal to install it")
		}
		fmt.Printf("Download and install %s now? [y/N] ", r.TagName)
		answer, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil {
			return err
		}
		if !strings.EqualFold(strings.TrimSpace(answer), "y") && !strings.EqualFold(strings.TrimSpace(answer), "yes") {
			fmt.Println("update cancelled")
			return nil
		}
	}
	name := "trellis_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	for _, a := range r.Assets {
		if a.Name == name {
			return installAsset(ctx, a.URL)
		}
	}
	return fmt.Errorf("release %s has no asset %s", r.TagName, name)
}

func installAsset(ctx context.Context, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download release: %s", resp.Status)
	}
	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if filepath.Base(h.Name) != "trellis" && filepath.Base(h.Name) != "trellis.exe" {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(tr, 64<<20))
		if err != nil {
			return err
		}
		target, err := os.Executable()
		if err != nil {
			return err
		}
		// Replace the real file, not a symlink pointing at it: overwriting the
		// link would leave the installed binary untouched.
		if resolved, err := filepath.EvalSymlinks(target); err == nil {
			target = resolved
		}
		tmp := target + ".new"
		if err := os.WriteFile(tmp, data, 0o755); err != nil {
			return err
		}
		// Windows will not let a running image be replaced, but it will let it
		// be renamed. Move the current binary aside first and delete it once
		// the new one is in place; a failure to remove is not fatal, because
		// the next update overwrites the same path.
		old := target + ".old"
		_ = os.Remove(old)
		moved := false
		if runtime.GOOS == "windows" {
			if err := os.Rename(target, old); err != nil {
				_ = os.Remove(tmp)
				return err
			}
			moved = true
		}
		if err := os.Rename(tmp, target); err != nil {
			_ = os.Remove(tmp)
			if moved {
				_ = os.Rename(old, target)
			}
			return err
		}
		if moved {
			_ = os.Remove(old)
		}
		fmt.Println("updated", target)
		return nil
	}
	return fmt.Errorf("release archive did not contain trellis")
}
