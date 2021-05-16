// Copyright 2021 Flamego. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package session

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

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
	f.Get("/destroy", func(session Session, store Store) error {
		return store.Destroy(session.ID())
	})

	resp := httptest.NewRecorder()
	req, err := http.NewRequest("GET", "/set", nil)
	assert.Nil(t, err)

	f.ServeHTTP(resp, req)
	assert.Equal(t, http.StatusOK, resp.Code)

	cookie := resp.Header().Get("Set-Cookie")

	resp = httptest.NewRecorder()
	req, err = http.NewRequest("GET", "/get", nil)
	assert.Nil(t, err)

	req.Header.Set("Cookie", cookie)
	f.ServeHTTP(resp, req)
	assert.Equal(t, http.StatusOK, resp.Code)

	resp = httptest.NewRecorder()
	req, err = http.NewRequest("GET", "/destroy", nil)
	assert.Nil(t, err)

	req.Header.Set("Cookie", cookie)
	f.ServeHTTP(resp, req)
	assert.Equal(t, http.StatusOK, resp.Code)
}

func TestMemoryStore_GC(t *testing.T) {
	now := time.Now()
	store := newMemoryStore(
		MemoryConfig{
			nowFunc:  func() time.Time { return now },
			Lifetime: time.Second,
		},
	)

	sess1, err := store.Read("1")
	assert.Nil(t, err)

	now = now.Add(-time.Second)
	sess2, err := store.Read("2")
	assert.Nil(t, err)

	now = now.Add(-2 * time.Second)
	_, err = store.Read("3")
	assert.Nil(t, err)

	now = now.Add(2 * time.Second)
	err = store.GC() // sess3 should be recycled
	assert.Nil(t, err)

	wantHeap := []*memorySession{sess2.(*memorySession), sess1.(*memorySession)}
	assert.Equal(t, wantHeap, store.heap)

	wantIndex := map[string]*memorySession{
		"1": sess1.(*memorySession),
		"2": sess2.(*memorySession),
	}
	assert.Equal(t, wantIndex, store.index)
}
