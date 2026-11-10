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
	"strings"
	"testing"
	"time"

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
	f.Get("/regenerate", func(w http.ResponseWriter, r *http.Request, session Session) string {
		err := session.RegenerateID(w, r)
		require.NoError(t, err)
		return "something in the response body"
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
	return newMemorySession(sid, nil), nil
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

func TestSessioner_Deadline(t *testing.T) {
	signedInAt := time.Date(2026, 9, 20, 23, 54, 59, 0, time.UTC)
	deadline := time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)
	now := signedInAt
	rootDir := t.TempDir()

	// Each call returns an app with its own session store over the same
	// directory, which is like a restart or another instance of the application.
	newApp := func() *flamego.Flame {
		f := flamego.NewWithLogger(&bytes.Buffer{})
		f.Use(Sessioner(
			Options{
				Initer:  FileIniter(),
				Config:  FileConfig{RootDir: rootDir},
				nowFunc: func() time.Time { return now },
			},
		))
		show := func(s Session) string {
			deadline, ok := Deadline(s)
			return fmt.Sprintf("%s %v %v %s", s.ID(), s.Get("user_id"), ok, deadline.Format(time.RFC3339Nano))
		}
		f.Get("/sign-in", func(s Session) string {
			s.Set("user_id", 1)
			SetDeadline(s, deadline)
			return show(s)
		})
		f.Get("/visit", func(s Session) string {
			s.Set("visited_at", now.UnixNano())
			return show(s)
		})
		f.Get("/flush", func(s Session) string {
			s.Flush()
			s.Set("user_id", 1)
			return show(s)
		})
		f.Get("/slow", func(c flamego.Context, s Session, store Store) {
			s.Set("visited_at", now.UnixNano())
			// The deadline passes while handling the request, and a concurrent request
			// has destroyed the session already.
			now = deadline
			require.NoError(t, store.Destroy(c.Request().Context(), s.ID()))
		})
		f.Get("/", func(c flamego.Context, s Session, store Store) string {
			sid, _ := c.Request().Cookie("flamego_session")
			return fmt.Sprintf("%s %v", show(s), store.Exist(c.Request().Context(), sid.Value))
		})
		return f
	}
	request := func(path, cookie string) *httptest.ResponseRecorder {
		resp := httptest.NewRecorder()
		req, err := http.NewRequest(http.MethodGet, path, nil)
		require.NoError(t, err)
		req.Header.Set("Cookie", cookie)
		newApp().ServeHTTP(resp, req)
		require.Equal(t, http.StatusOK, resp.Code)
		return resp
	}
	signIn := func() (cookie, body string) {
		now = signedInAt
		resp := request("/sign-in", "")
		body = resp.Body.String()
		assert.Equal(t, "1 true 2026-09-21T00:00:00Z", body[strings.Index(body, " ")+1:])
		return resp.Header().Get("Set-Cookie"), body
	}
	assertReplaced := func(t *testing.T, resp *httptest.ResponseRecorder, cookie string) {
		t.Helper()
		newCookie := resp.Header().Get("Set-Cookie")
		assert.NotEmpty(t, newCookie)
		assert.NotEqual(t, cookie, newCookie)
		sid := resp.Body.String()[:16]
		assert.NotEqual(t, cookie[len("flamego_session="):][:16], sid)
		assert.Equal(t, sid+" <nil> false 0001-01-01T00:00:00Z false", resp.Body.String())
	}

	t.Run("before and at the deadline", func(t *testing.T) {
		cookie, signedIn := signIn()

		// Requests before the deadline, saving data or not, keep the session and
		// do not move the deadline. Flush keeps the deadline.
		now = deadline.Add(-time.Nanosecond)
		resp := request("/visit", cookie)
		assert.Equal(t, signedIn, resp.Body.String())
		assert.Empty(t, resp.Header().Get("Set-Cookie"))
		resp = request("/flush", cookie)
		assert.Equal(t, signedIn, resp.Body.String())
		resp = request("/", cookie)
		assert.Equal(t, signedIn+" true", resp.Body.String())

		// At the deadline, the old session is destroyed and replaced by a new one
		// even though the client still sends the old session ID.
		now = deadline
		assertReplaced(t, request("/", cookie), cookie)
	})

	t.Run("deadline passes during the request", func(t *testing.T) {
		cookie, signedIn := signIn()
		request("/slow", cookie)

		// Go back in time to see whether the old session was written back.
		now = signedInAt
		resp := request("/", cookie)
		assert.Equal(t, signedIn[:16]+" <nil> false 0001-01-01T00:00:00Z false", resp.Body.String())
	})

	t.Run("unreadable deadline", func(t *testing.T) {
		cookie, _ := signIn()

		// For example, a custom Decoder that returns a different type.
		ctx := context.Background()
		store, err := FileIniter()(ctx, FileConfig{RootDir: rootDir}, IDWriter(func(http.ResponseWriter, *http.Request, string) {}))
		require.NoError(t, err)
		sess, err := store.Read(ctx, cookie[len("flamego_session="):][:16])
		require.NoError(t, err)
		sess.Set(deadlineKey, deadline.Add(time.Hour).UnixNano())
		require.NoError(t, store.Save(ctx, sess))

		assertReplaced(t, request("/", cookie), cookie)
	})
}

func TestDeadline(t *testing.T) {
	// Years outside the range of int64 nanoseconds since the Unix epoch.
	for _, want := range []time.Time{
		time.Date(1600, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2300, 1, 1, 0, 0, 0, 1, time.UTC),
	} {
		s := NewBaseSession("abc", nil, nil)
		SetDeadline(s, want)
		got, ok := Deadline(s)
		assert.True(t, ok)
		assert.True(t, want.Equal(got), "want %v, got %v", want, got)
	}
}
