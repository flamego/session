// Copyright 2021 Flamego. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package session

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/flamego/flamego"
)

func TestSessioner(t *testing.T) {
	f := flamego.NewWithLogger(&bytes.Buffer{})
	f.Use(Sessioner())
	f.Get("/", func(c flamego.Context, session Session, store Store) string {
		_ = store.GC(c.Request().Context())
		return session.ID()
	})

	resp := httptest.NewRecorder()
	req, err := http.NewRequest("GET", "/", nil)
	assert.Nil(t, err)

	f.ServeHTTP(resp, req)

	want := fmt.Sprintf("flamego_session=%s; Path=/; HttpOnly; SameSite=Lax", resp.Body.String())
	cookie := resp.Header().Get("Set-Cookie")
	assert.Equal(t, want, cookie)

	// Make a request again using the same session ID
	resp = httptest.NewRecorder()
	req, err = http.NewRequest(http.MethodGet, "/", nil)
	assert.Nil(t, err)

	req.Header.Set("Cookie", cookie)
	f.ServeHTTP(resp, req)

	got := fmt.Sprintf("flamego_session=%s; Path=/; HttpOnly; SameSite=Lax", resp.Body.String())
	assert.Equal(t, cookie, got)
}

func TestSessioner_Header(t *testing.T) {
	f := flamego.NewWithLogger(&bytes.Buffer{})
	f.Use(Sessioner(
		Options{
			ReadIDFunc: func(r *http.Request) string {
				return r.Header.Get("Session-Id")
			},
			WriteIDFunc: func(w http.ResponseWriter, r *http.Request, sid string, created bool) {
				if created {
					r.Header.Set("Session-Id", sid)
				}
				w.Header().Set("Session-Id", sid)
			},
		},
	))
	f.Get("/", func(c flamego.Context, session Session, store Store) string {
		_ = store.GC(c.Request().Context())
		return session.ID()
	})

	resp := httptest.NewRecorder()
	req, err := http.NewRequest(http.MethodGet, "/", nil)
	assert.Nil(t, err)

	f.ServeHTTP(resp, req)

	sid := resp.Header().Get("Session-Id")
	assert.Equal(t, resp.Body.String(), sid)

	// Make a request again using the same session ID
	resp = httptest.NewRecorder()
	req, err = http.NewRequest(http.MethodGet, "/", nil)
	assert.Nil(t, err)

	req.Header.Set("Session-Id", sid)
	f.ServeHTTP(resp, req)

	assert.Equal(t, sid, resp.Body.String())
}

func TestSession_Flash(t *testing.T) {
	f := flamego.NewWithLogger(&bytes.Buffer{})
	f.Use(Sessioner())
	f.Get("/", func(c flamego.Context, f Flash) string {
		s, ok := f.(string)
		if !ok {
			return "no flash"
		}
		return s
	})
	f.Post("/set-flash", func(s Session) {
		s.SetFlash("This is a flash message")
	})

	// No flash in the initial request
	resp := httptest.NewRecorder()
	req, err := http.NewRequest("GET", "/", nil)
	assert.Nil(t, err)

	f.ServeHTTP(resp, req)

	assert.Equal(t, "no flash", resp.Body.String())

	cookie := resp.Header().Get("Set-Cookie")

	// Send a request to set flash
	resp = httptest.NewRecorder()
	req, err = http.NewRequest(http.MethodPost, "/set-flash", nil)
	assert.Nil(t, err)

	req.Header.Set("Cookie", cookie)
	f.ServeHTTP(resp, req)

	// Flash should be returned
	resp = httptest.NewRecorder()
	req, err = http.NewRequest("GET", "/", nil)
	assert.Nil(t, err)

	req.Header.Set("Cookie", cookie)
	f.ServeHTTP(resp, req)

	assert.Equal(t, "This is a flash message", resp.Body.String())

	// Flash has gone now if we try again
	resp = httptest.NewRecorder()
	req, err = http.NewRequest("GET", "/", nil)
	assert.Nil(t, err)

	req.Header.Set("Cookie", cookie)
	f.ServeHTTP(resp, req)

	assert.Equal(t, "no flash", resp.Body.String())
}
