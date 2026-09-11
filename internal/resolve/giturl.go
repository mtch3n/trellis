// Package resolve turns a working directory into a project identity.
package resolve

import (
	"net/url"
	"strings"
	"unicode"
)

// isHostAlias marks a host that looks like an SSH config alias
// ("bitbucket.org-gojitech"). Such a host cannot be mapped to a real one, so
// the caller must fall back to the root path or a .trellis pin.
//
// The tell is that an alias puts a dash where the TLD belongs:
// "bitbucket.org-gojitech" ends in "org-gojitech", which is not a TLD.
// Do NOT test the first dash instead -- "code.my-company.com" is an ordinary
// host whose first dash sits inside a label, and rejecting it would silently
// fork one repository into a per-machine board.
func isHostAlias(host string) bool {
	_, last, ok := strings.CutLast(host, ".")
	if !ok {
		return false
	}
	if rest, found := strings.CutPrefix(last, "xn--"); found {
		last = rest // punycode TLD: the remainder is alphanumeric
	}
	for _, r := range last {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return true
		}
	}
	// An empty final label cannot occur: NormalizeRemote trims the trailing
	// dot of an absolute FQDN before calling this.
	return false
}

// NormalizeRemote reduces a git remote URL to a stable "host/owner/repo"
// identity. It returns ok=false when the URL cannot be reduced.
func NormalizeRemote(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}

	var host, path string
	switch {
	case strings.Contains(raw, "://"):
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" {
			return "", false
		}
		host, path = u.Hostname(), u.Path
	default:
		// scp-like form: [user@]host:path
		hostPart, rest, ok := strings.Cut(raw, ":")
		if !ok {
			return "", false
		}
		if _, h, found := strings.Cut(hostPart, "@"); found {
			hostPart = h
		}
		host, path = hostPart, rest
	}

	// "example.com." is the absolute form of "example.com". Trim it, so the
	// same repository cloned either way yields one identity rather than two.
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if !strings.Contains(host, ".") || isHostAlias(host) {
		return "", false
	}

	path = strings.Trim(path, "/")
	path = strings.ToLower(strings.TrimSuffix(path, ".git"))
	if path == "" || !strings.Contains(path, "/") {
		return "", false
	}
	return host + "/" + path, true
}
