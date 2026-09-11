package daemon

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

func TestIPCAuthentication(t *testing.T) {
	for _, token := range []string{"", "wrong", "secret"} {
		t.Run("token="+token, func(t *testing.T) {
			server, client := net.Pipe()
			defer client.Close()
			invoked := make(chan struct{}, 1)
			go serveConnContext(t.Context(), server, func(context.Context, Request) (Response, error) {
				invoked <- struct{}{}
				return Response{OK: true}, nil
			}, "secret")
			resp, err := callConn(client, Request{Method: "health", Token: token})
			if token == "secret" {
				if err != nil || !resp.OK {
					t.Fatalf("authenticated request: %v", err)
				}
				<-invoked
			} else {
				if err == nil || !strings.Contains(err.Error(), "unauthorized") {
					t.Fatalf("unauthorized request: %v", err)
				}
				select {
				case <-invoked:
					t.Fatal("handler invoked without auth")
				default:
				}
			}
		})
	}
}

func TestIPCCancellationStopsHandler(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	entered, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		serveConnContext(ctx, server, func(ctx context.Context, _ Request) (Response, error) {
			close(entered)
			<-ctx.Done()
			return Response{}, ctx.Err()
		}, "")
	}()
	go func() { _, _ = callConn(client, Request{Method: "search"}) }()
	<-entered
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("handler ignored shutdown cancellation")
	}
}

func TestIPCRejectsEmptyResponse(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()
	go func() { var b [1024]byte; _, _ = server.Read(b[:]); server.Close() }()
	if _, err := callConn(client, Request{Method: "health"}); err == nil {
		t.Fatal("empty response reported success")
	}
}
