// Copyright 2021 Flamego. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package session

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flamego/flamego"
)

func TestMemoryStore(t *testing.T) {
	f := flamego.NewWithLogger(&bytes.Buffer{})
	f.Use(Sessioner())

	f.Get("/set", func(s Session) {
		s.Set("username", "flamego")
	})
	f.Get("/get", func(s Session) {
		sid := s.ID()
		assert.Len(t, sid, 16)

		username, ok := s.Get("username").(string)
		assert.True(t, ok)
		assert.Equal(t, "flamego", username)

		s.Delete("username")
		_, ok = s.Get("username").(string)
		assert.False(t, ok)

		s.Set("random", "value")
		s.Flush()
		_, ok = s.Get("random").(string)
		assert.False(t, ok)
	})
	f.Get("/destroy", func(c flamego.Context, session Session, store Store) error {
		return store.Destroy(c.Request().Context(), session.ID())
	})

	resp := httptest.NewRecorder()
	req, err := http.NewRequest(http.MethodGet, "/set", nil)
	require.Nil(t, err)

	f.ServeHTTP(resp, req)
	assert.Equal(t, http.StatusOK, resp.Code)

	cookie := resp.Header().Get("Set-Cookie")

	resp = httptest.NewRecorder()
	req, err = http.NewRequest(http.MethodGet, "/get", nil)
	require.Nil(t, err)

	req.Header.Set("Cookie", cookie)
	f.ServeHTTP(resp, req)
	assert.Equal(t, http.StatusOK, resp.Code)

	resp = httptest.NewRecorder()
	req, err = http.NewRequest(http.MethodGet, "/destroy", nil)
	require.Nil(t, err)

	req.Header.Set("Cookie", cookie)
	f.ServeHTTP(resp, req)
	assert.Equal(t, http.StatusOK, resp.Code)
}

func TestMemoryStore_GC(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	store := newMemoryStore(
		MemoryConfig{
			nowFunc:  func() time.Time { return now },
			Lifetime: time.Second,
		},
		nil,
	)

	sess1, err := store.Read(ctx, "1")
	require.Nil(t, err)

	now = now.Add(-2 * time.Second)
	sess2, err := store.Read(ctx, "2")
	require.Nil(t, err)

	sess2.Set("name", "flamego")
	err = store.Save(ctx, sess2)
	require.Nil(t, err)

	// Read on an expired session should wipe data but preserve the record
	now = now.Add(2 * time.Second)
	tmp, err := store.Read(ctx, "2")
	require.Nil(t, err)
	assert.Nil(t, tmp.Get("name"))

	now = now.Add(-2 * time.Second)
	_, err = store.Read(ctx, "3")
	require.Nil(t, err)

	now = now.Add(2 * time.Second)
	err = store.GC(ctx) // sess3 should be recycled
	require.Nil(t, err)

	wantHeap := []*memorySession{sess2.(*memorySession), sess1.(*memorySession)}
	assert.Equal(t, wantHeap, store.heap)

	wantIndex := map[string]*memorySession{
		"1": sess1.(*memorySession),
		"2": sess2.(*memorySession),
	}
	assert.Equal(t, wantIndex, store.index)
}

func TestMemoryStore_Touch(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	store := newMemoryStore(
		MemoryConfig{
			nowFunc:  func() time.Time { return now },
			Lifetime: time.Second,
		},
		nil,
	)

	sess, err := store.Read(ctx, "1")
	require.Nil(t, err)

	now = now.Add(2 * time.Second)
	// Touch should keep the session alive
	err = store.Touch(ctx, sess.ID())
	require.Nil(t, err)

	err = store.GC(ctx)
	require.Nil(t, err)

	wantHeap := []*memorySession{sess.(*memorySession)}
	assert.Equal(t, wantHeap, store.heap)
}
