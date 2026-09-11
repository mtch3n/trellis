package ui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPBoundaryRequiresLocalHostSessionAndSameOrigin(t *testing.T) {
	s := &Server{token: "private-test-token", mux: http.NewServeMux()}
	s.mux.HandleFunc("/api/test", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	handler := s.protectedHandler("127.0.0.1:7788")
	cases := []struct {
		name, host, origin, token, contentType, method string
		want                                           int
	}{
		{"anonymous", "127.0.0.1:7788", "", "", "", "GET", 401},
		{"foreign host", "evil.example:7788", "", s.token, "application/json", "POST", 403},
		{"foreign origin", "127.0.0.1:7788", "https://evil.example", s.token, "application/json", "POST", 403},
		{"plain text", "127.0.0.1:7788", "", s.token, "text/plain", "POST", 415},
		{"bad token", "127.0.0.1:7788", "", "wrong", "application/json", "POST", 401},
		{"authorized read", "127.0.0.1:7788", "", s.token, "", "GET", 204},
		{"authorized write", "127.0.0.1:7788", "http://127.0.0.1:7788", s.token, "application/json", "POST", 204},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "http://"+tc.host+"/api/test", strings.NewReader("{}"))
			req.Header.Set("Origin", tc.origin)
			req.Header.Set("X-Trellis-Token", tc.token)
			req.Header.Set("Content-Type", tc.contentType)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status=%d want=%d: %s", rec.Code, tc.want, rec.Body)
			}
		})
	}
	bootstrap := httptest.NewRequest("GET", s.URL("127.0.0.1:7788"), nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, bootstrap)
	if rec.Code != 303 || rec.Header().Get("Location") != "/" {
		t.Fatalf("bootstrap: %d %v", rec.Code, rec.Header())
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("insecure cookie: %#v", cookies)
	}
	req := httptest.NewRequest("GET", "http://127.0.0.1:7788/api/test", nil)
	req.AddCookie(cookies[0])
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != 204 {
		t.Fatalf("session cookie rejected: %d", rec.Code)
	}
}
