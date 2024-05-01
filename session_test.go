// Copyright 2021 Flamego. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package session

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flamego/flamego"
)

func TestSessioner(t *testing.T) {
	f := flamego.NewWithLogger(&bytes.Buffer{})
	f.Use(Sessioner())
	f.Get("/", func(c flamego.Context, session Session, store Store) string {
		_ = store.GC(c.Request().Context())
		return session.ID()
	})
	f.Get("/regenerate", func(session Session) {
		err := session.RegenerateID()
		require.NoError(t, err)
	})

	resp := httptest.NewRecorder()
	req, err := http.NewRequest(http.MethodGet, "/", nil)
	require.NoError(t, err)

	f.ServeHTTP(resp, req)

	want := fmt.Sprintf("flamego_session=%s; Path=/; HttpOnly; SameSite=Lax", resp.Body.String())
	cookie := resp.Header().Get("Set-Cookie")
	assert.Equal(t, want, cookie)

	// Make a request again using the same session ID
	resp = httptest.NewRecorder()
	req, err = http.NewRequest(http.MethodGet, "/", nil)
	require.NoError(t, err)

	req.Header.Set("Cookie", cookie)
	f.ServeHTTP(resp, req)

	got := fmt.Sprintf("flamego_session=%s; Path=/; HttpOnly; SameSite=Lax", resp.Body.String())
	assert.Equal(t, cookie, got)

	// Force-regenerate the session ID even if the session ID exists.
	resp = httptest.NewRecorder()
	req, err = http.NewRequest(http.MethodGet, "/regenerate", nil)
	require.NoError(t, err)

	req.Header.Set("Cookie", cookie)
	f.ServeHTTP(resp, req)

	got = resp.Header().Get("Set-Cookie")
	assert.NotEmpty(t, got)
	assert.NotEqual(t, cookie, got)
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
	require.NoError(t, err)

	f.ServeHTTP(resp, req)

	sid := resp.Header().Get("Session-Id")
	assert.Equal(t, resp.Body.String(), sid)

	// Make a request again using the same session ID
	resp = httptest.NewRecorder()
	req, err = http.NewRequest(http.MethodGet, "/", nil)
	require.NoError(t, err)

	req.Header.Set("Session-Id", sid)
	f.ServeHTTP(resp, req)

	assert.Equal(t, sid, resp.Body.String())
}

type noopStore struct{}

func (s *noopStore) Exist(context.Context, string) bool {
	return false
}

func (s *noopStore) Read(_ context.Context, sid string) (Session, error) {
	return newMemorySession(sid), nil
}

func (s *noopStore) Destroy(context.Context, string) error {
	return nil
}

func (s *noopStore) Touch(context.Context, string) error {
	return nil
}

func (s *noopStore) Save(ctx context.Context, _ Session) error {
	if ctx.Err() != nil {
		return errors.Wrap(ctx.Err(), "something went wrong")
	}
	return nil
}

func (s *noopStore) GC(context.Context) error {
	return nil
}

func TestSessioner_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	f := flamego.NewWithLogger(&bytes.Buffer{})
	f.Use(Sessioner(
		Options{
			Initer: func(context.Context, ...interface{}) (Store, error) {
				return &noopStore{}, nil
			},
		},
	))
	f.Get("/", func() {
		cancel()
	})

	resp := httptest.NewRecorder()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/", nil)
	require.NoError(t, err)

	f.ServeHTTP(resp, req)
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
	req, err := http.NewRequest(http.MethodGet, "/", nil)
	require.NoError(t, err)

	f.ServeHTTP(resp, req)

	assert.Equal(t, "no flash", resp.Body.String())

	cookie := resp.Header().Get("Set-Cookie")

	// Send a request to set flash
	resp = httptest.NewRecorder()
	req, err = http.NewRequest(http.MethodPost, "/set-flash", nil)
	require.NoError(t, err)

	req.Header.Set("Cookie", cookie)
	f.ServeHTTP(resp, req)

	// Flash should be returned
	resp = httptest.NewRecorder()
	req, err = http.NewRequest(http.MethodGet, "/", nil)
	require.NoError(t, err)

	req.Header.Set("Cookie", cookie)
	f.ServeHTTP(resp, req)

	assert.Equal(t, "This is a flash message", resp.Body.String())

	// Flash has gone now if we try again
	resp = httptest.NewRecorder()
	req, err = http.NewRequest(http.MethodGet, "/", nil)
	require.NoError(t, err)

	req.Header.Set("Cookie", cookie)
	f.ServeHTTP(resp, req)

	assert.Equal(t, "no flash", resp.Body.String())
}
