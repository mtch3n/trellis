package ui

import (
	"context"
	"crypto/subtle"
	"fmt"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// URL carries the bootstrap credential once. The server exchanges it for an
// HttpOnly, same-site session cookie and redirects to a URL without credentials.
func (s *Server) URL(address string) string {
	return "http://" + address + "/?token=" + url.QueryEscape(s.token)
}

func (s *Server) protectedHandler(address string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		host, port, err := net.SplitHostPort(r.Host)
		_, wantPort, _ := net.SplitHostPort(address)
		ip := net.ParseIP(host)
		if err != nil || port != wantPort || (host != "localhost" && (ip == nil || !ip.IsLoopback())) {
			http.Error(w, "invalid local host", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || u.Scheme != "http" || u.Host != r.Host || u.User != nil {
				http.Error(w, "cross-origin request denied", http.StatusForbidden)
				return
			}
		}
		if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			http.Error(w, "cross-site request denied", http.StatusForbidden)
			return
		}
		if token := r.URL.Query().Get("token"); r.Method == http.MethodGet && r.URL.Path == "/" && token != "" {
			if !s.validToken(token) {
				http.Error(w, "invalid session token", http.StatusUnauthorized)
				return
			}
			http.SetCookie(w, &http.Cookie{Name: "trellis_session", Value: s.token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			token := r.Header.Get("X-Trellis-Token")
			if cookie, err := r.Cookie("trellis_session"); token == "" && err == nil {
				token = cookie.Value
			}
			if !s.validToken(token) {
				http.Error(w, "open the authenticated URL printed by trellis daemon", http.StatusUnauthorized)
				return
			}
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
				if err != nil || media != "application/json" {
					http.Error(w, "application/json required", http.StatusUnsupportedMediaType)
					return
				}
			}
			r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
		}
		s.mux.ServeHTTP(w, r)
	})
}

func (s *Server) validToken(token string) bool {
	return s.token != "" && subtle.ConstantTimeCompare([]byte(s.token), []byte(token)) == 1
}

func (s *Server) ServeContext(ctx context.Context, listener net.Listener) error {
	host, _, err := net.SplitHostPort(listener.Addr().String())
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		return fmt.Errorf("UI listener must use a loopback address")
	}
	server := &http.Server{Handler: s.protectedHandler(listener.Addr().String()), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, BaseContext: func(net.Listener) context.Context { return ctx }}
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		defer close(done)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
		}
	})
	defer func() {
		if !stop() {
			<-done
		}
	}()
	err = server.Serve(listener)
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}
