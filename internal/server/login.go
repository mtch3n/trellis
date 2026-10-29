package server

import (
	"net/http"

	"github.com/mtch3n/trellis/internal/platform"
)

const sessionCookie = "trellis_session"

type loginIn struct {
	Token string `json:"token"`
}

type meOut struct {
	Handle string `json:"handle"`
	Org    string `json:"org"`
	CSRF   string `json:"csrf"`
}

func (a *webAPI) login(w http.ResponseWriter, r *http.Request) {
	var in loginIn
	if err := readJSON(r, &in); err != nil {
		writeError(w, err)
		return
	}
	secret, err := a.p.StartSession(r.Context(), in.Token)
	if platform.IsCode(err, "bad_token") || platform.IsCode(err, "not_person_token") {
		writeErrorStatus(w, http.StatusUnauthorized, err)
		return
	}
	if err != nil {
		writeError(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: secret, Path: "/", MaxAge: int(platform.SessionTTL.Seconds()),
		HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: r.TLS != nil,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (a *webAPI) logout(c webCall) (any, error) {
	if err := a.p.EndSession(c.r.Context(), c.secret); err != nil {
		return nil, err
	}
	http.SetCookie(c.w, &http.Cookie{
		Name: sessionCookie, Path: "/", MaxAge: -1,
		HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: c.r.TLS != nil,
	})
	return struct{}{}, nil
}

func (a *webAPI) me(c webCall) (any, error) {
	return meOut{Handle: c.s.Handle, Org: c.s.Org, CSRF: c.s.CSRF}, nil
}
