package daemon

import (
	"context"
	"net"
	"testing"
)

func TestProtocolRoundTrip(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	done := make(chan struct{})
	go func() {
		serveConn(server, func(_ context.Context, req Request) (Response, error) {
			if req.Method != "health" {
				t.Errorf("method=%q", req.Method)
			}
			return Response{OK: true}, nil
		})
		close(done)
	}()
	resp, err := callConn(client, Request{Protocol: ProtocolVersion, Method: "health"})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.OK || resp.Protocol != ProtocolVersion {
		t.Fatalf("response=%#v", resp)
	}
	<-done
}
