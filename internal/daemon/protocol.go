// Package daemon defines the private, versioned local IPC protocol.
package daemon

import (
	"bufio"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/mtch3n/trellis/internal/core"
)

type Request struct {
	Protocol     int    `json:"protocol"`
	Method       string `json:"method"`
	SearchMethod string `json:"search_method,omitempty"`
	Token        string `json:"token,omitempty"`
	Query        string `json:"query,omitempty"`
	ProjectID    string `json:"project_id,omitempty"`
	AllProjects  bool   `json:"all_projects,omitempty"`
	Label        string `json:"label,omitempty"`
	Limit        int    `json:"limit,omitempty"`
}

type Response struct {
	Protocol int              `json:"protocol"`
	OK       bool             `json:"ok"`
	Error    string           `json:"error,omitempty"`
	Results  []core.SearchHit `json:"results,omitempty"`
	Data     map[string]any   `json:"data,omitempty"`
}

type Handler func(context.Context, Request) (Response, error)

// authenticatedListener protects Windows TCP IPC, where filesystem socket
// permissions cannot authenticate the connecting user.
type authenticatedListener struct {
	net.Listener
	token string
}

func Serve(listener net.Listener, handler Handler) error {
	return ServeContext(context.Background(), listener, handler)
}

func ServeContext(ctx context.Context, listener net.Listener, handler Handler) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(ctx, func() { _ = listener.Close() })
	defer stop()
	var wg sync.WaitGroup
	defer wg.Wait()
	// Cancel connection handlers before waiting for them, including Accept errors.
	defer cancel()
	token := ""
	if l, ok := listener.(*authenticatedListener); ok {
		token = l.token
	}
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		wg.Go(func() { serveConnContext(ctx, conn, handler, token) })
	}
}

const requestTimeout = 2 * time.Minute

func serveConn(conn net.Conn, handler Handler) {
	serveConnContext(context.Background(), conn, handler, "")
}

func serveConnContext(parent context.Context, conn net.Conn, handler Handler, token string) {
	defer conn.Close()
	ctx, cancel := context.WithTimeout(parent, requestTimeout)
	defer cancel()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	out := json.NewEncoder(conn)
	var req Request
	if err := json.NewDecoder(io.LimitReader(conn, 1<<20)).Decode(&req); err != nil {
		_ = out.Encode(Response{Protocol: ProtocolVersion, Error: err.Error()})
		return
	}
	_ = conn.SetDeadline(time.Now().Add(requestTimeout))
	if token != "" && subtle.ConstantTimeCompare([]byte(req.Token), []byte(token)) != 1 {
		_ = out.Encode(Response{Protocol: ProtocolVersion, Error: "unauthorized daemon client"})
		return
	}
	if req.Protocol == 0 {
		req.Protocol = ProtocolVersion
	}
	if req.Protocol != ProtocolVersion {
		_ = out.Encode(Response{Protocol: ProtocolVersion, Error: fmt.Sprintf("unsupported daemon protocol %d", req.Protocol)})
		return
	}
	resp, err := handler(ctx, req)
	if resp.Protocol == 0 {
		resp.Protocol = ProtocolVersion
	}
	if err != nil {
		resp = Response{Protocol: ProtocolVersion, Error: err.Error()}
	}
	_ = out.Encode(resp)
}

func Call(ctx context.Context, socket string, req Request) (Response, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	network, address := "unix", socket
	if runtime.GOOS == "windows" {
		data, err := os.ReadFile(socket)
		if err != nil {
			return Response{}, fmt.Errorf("read daemon endpoint: %w", err)
		}
		var endpoint struct {
			Address string `json:"address"`
			Token   string `json:"token"`
		}
		if err := json.Unmarshal(data, &endpoint); err != nil {
			return Response{}, err
		}
		if endpoint.Token == "" {
			return Response{}, fmt.Errorf("daemon endpoint has no authentication token; restart the daemon")
		}
		network, address, req.Token = "tcp", endpoint.Address, endpoint.Token
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, network, address)
	if err != nil {
		return Response{}, fmt.Errorf("connect daemon socket: %w", err)
	}
	defer conn.Close()
	deadline, _ := ctx.Deadline()
	_ = conn.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	resp, err := callConn(conn, req)
	if ctx.Err() != nil {
		return Response{}, ctx.Err()
	}
	return resp, err
}

func callConn(conn net.Conn, req Request) (Response, error) {
	if req.Protocol == 0 {
		req.Protocol = ProtocolVersion
	}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return Response{}, err
	}
	var resp Response
	if err := json.NewDecoder(bufio.NewReader(conn)).Decode(&resp); err != nil {
		return Response{}, err
	}
	if resp.Protocol != ProtocolVersion {
		return resp, fmt.Errorf("unsupported daemon protocol %d", resp.Protocol)
	}
	if !resp.OK {
		if resp.Error == "" {
			resp.Error = "request failed"
		}
		return resp, fmt.Errorf("daemon: %s", resp.Error)
	}
	return resp, nil
}
