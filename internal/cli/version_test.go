package cli

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"
)

type countingTransport struct{ calls int }

func (c *countingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	c.calls++
	return nil, errors.New("network disabled in test")
}

// Callers poll `version`; it must answer locally with exactly one JSON value.
func TestVersionIsLocalAndJSONOnly(t *testing.T) {
	rt := &countingTransport{}
	saved := http.DefaultClient.Transport
	http.DefaultClient.Transport = rt
	t.Cleanup(func() { http.DefaultClient.Transport = saved })

	out := runCmd(t, "version", "--json")
	var v map[string]string
	if err := json.Unmarshal([]byte(out), &v); err != nil || v["version"] == "" {
		t.Fatalf("version --json = %q, not one JSON object: %v", out, err)
	}
	if rt.calls != 0 {
		t.Errorf("plain version made %d network requests", rt.calls)
	}
}
