package server

import (
	"net"
	"strings"
	"testing"

	"github.com/mtch3n/trellis/internal/pgstore/pgtest"
)

func TestPlat_PLAT_C30_ServeOnABusyAddressExitsNamingTheAddress(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	addr := busy.Addr().String()

	env := map[string]string{"TRELLIS_DATABASE_URL": pgtest.URL(t), "TRELLIS_ADDR": addr}
	listened := false
	err = Run(t.Context(), func(k string) string { return env[k] }, func(net.Addr) { listened = true })
	if err == nil {
		t.Fatal("serve succeeded on a busy address")
	}
	if !strings.Contains(err.Error(), addr) {
		t.Fatalf("the message does not name %s: %q", addr, err)
	}
	if listened {
		t.Fatal("serve reported that it listened")
	}
}
