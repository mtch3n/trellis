package resolve

import "testing"

func TestNormalizeRemote(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{"scp form", "git@github.com:mtch3n/xpsctl.git", "github.com/mtch3n/xpsctl", true},
		{"scp no suffix", "git@github.com:mtch3n/xpsctl", "github.com/mtch3n/xpsctl", true},
		{"ssh url", "ssh://git@github.com/mtch3n/xpsctl.git", "github.com/mtch3n/xpsctl", true},
		{"https", "https://github.com/mtch3n/xpsctl.git", "github.com/mtch3n/xpsctl", true},
		{"https no suffix", "https://github.com/mtch3n/xpsctl", "github.com/mtch3n/xpsctl", true},
		{"credentials stripped", "https://user:token@github.com/mtch3n/xpsctl.git", "github.com/mtch3n/xpsctl", true},
		{"trailing slash", "https://github.com/mtch3n/xpsctl/", "github.com/mtch3n/xpsctl", true},
		{"case folded", "https://GitHub.com/MTch3n/XPSctl.git", "github.com/mtch3n/xpsctl", true},
		{"port", "ssh://git@github.com:22/mtch3n/xpsctl.git", "github.com/mtch3n/xpsctl", true},
		{"nested group", "https://gitlab.com/a/b/c.git", "gitlab.com/a/b/c", true},

		// Host aliases cannot be normalized: the alias is not the real host.
		{"ssh host alias", "git@bitbucket.org-gojitech:gojitech/labk8s.git", "", false},
		{"empty", "", "", false},
		{"local path", "/srv/git/thing.git", "", false},
		{"no repo", "https://github.com/", "", false},

		// Ordinary hosts whose dash is INSIDE a label must NOT be mistaken for
		// aliases; rejecting them forks one repository into per-machine boards.
		{"dash in middle label", "git@code.my-company.com:team/repo.git", "code.my-company.com/team/repo", true},
		{"dash in middle label https", "https://git.my-org.io/a/b.git", "git.my-org.io/a/b", true},
		{"dash in first label", "git@git-codecommit.us-east-1.amazonaws.com:v1/repos/x", "git-codecommit.us-east-1.amazonaws.com/v1/repos/x", true},

		// An absolute FQDN must normalize to the same identity as the relative
		// form, or one repository forks into two boards.
		{"trailing dot host", "https://example.com./a/b.git", "example.com/a/b", true},
		{"trailing dot scp", "git@example.com.:a/b.git", "example.com/a/b", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := NormalizeRemote(tc.in)
			if ok != tc.ok || got != tc.want {
				t.Errorf("NormalizeRemote(%q) = (%q, %v), want (%q, %v)", tc.in, got, ok, tc.want, tc.ok)
			}
		})
	}
}
