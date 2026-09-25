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
			upload := r.Method == http.MethodPost && isUploadPath(r.URL.Path)
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
				switch {
				case err == nil && media == "application/json":
				case upload && err == nil && media == "multipart/form-data":
					// An artifact is a file the person at the browser picked,
					// and a file field cannot be sent as JSON. The token, the
					// loopback host and the same-origin checks above still
					// apply, and the session cookie is SameSite=Strict, so a
					// cross-site form post carries no credential.
				default:
					http.Error(w, "application/json required", http.StatusUnsupportedMediaType)
					return
				}
			}
			limit := int64(2 << 20)
			switch {
			case upload:
				limit = artifactUploadLimit
			case isProxyPath(r.URL.Path):
				// A chat resends its whole conversation every turn.
				limit = proxyBodyLimit
			}
			r.Body = http.MaxBytesReader(w, r.Body, limit)
		}
		s.mux.ServeHTTP(w, r)
	})
}

// isUploadPath reports whether a path is the one route that takes a file
// rather than JSON: POST /api/p/{key}/artifacts.
func isUploadPath(path string) bool {
	rest, ok := strings.CutPrefix(path, "/api/p/")
	if !ok {
		return false
	}
	key, tail, ok := strings.Cut(rest, "/")
	return ok && key != "" && tail == "artifacts"
}

// proxyBodyLimit bounds one request forwarded to a provider.
const proxyBodyLimit = 32 << 20

// isProxyPath reports whether a path is forwarded to a provider:
// /api/providers/{id}/proxy/...
func isProxyPath(path string) bool {
	rest, ok := strings.CutPrefix(path, "/api/providers/")
	if !ok {
		return false
	}
	_, tail, ok := strings.Cut(rest, "/")
	return ok && strings.HasPrefix(tail, "proxy/")
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
