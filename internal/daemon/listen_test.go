package daemon

import (
	"context"
	"strings"
	"testing"
)

func TestListenCallAndCleanup(t *testing.T) {
	root := t.TempDir()
	listener, cleanup, err := Listen(root)
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skip("sandbox does not permit Unix-domain sockets")
		}
		t.Fatal(err)
	}
	defer cleanup()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = Serve(listener, func(_ context.Context, req Request) (Response, error) {
			if req.Method != "health" {
				t.Errorf("method = %q, want health", req.Method)
			}
			return Response{OK: true}, nil
		})
	}()

	resp, err := Call(t.Context(), Endpoint(root), Request{Method: "health"})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.OK || resp.Protocol != ProtocolVersion {
		t.Fatalf("response = %#v", resp)
	}

	cleanup()
	<-done
}
