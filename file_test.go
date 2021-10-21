// Copyright 2021 Flamego. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package session

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/flamego/flamego"
)

func TestFileStore(t *testing.T) {
	f := flamego.NewWithLogger(&bytes.Buffer{})
	f.Use(Sessioner(
		Options{
			Initer: FileIniter(),
			Config: FileConfig{
				nowFunc: time.Now,
				RootDir: filepath.Join(os.TempDir(), "sessions"),
			},
		},
	))

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

func TestFileStore_GC(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	store, err := FileIniter()(ctx,
		FileConfig{
			nowFunc:  func() time.Time { return now },
			RootDir:  filepath.Join(os.TempDir(), "sessions"),
			Lifetime: time.Second,
		},
	)
	assert.Nil(t, err)

	setModTime := func(sid string) {
		t.Helper()

		err := os.Chtimes(store.(*fileStore).filename(sid), now, now)
		assert.Nil(t, err)
	}

	sess1, err := store.Read(ctx, "111")
	assert.Nil(t, err)
	err = store.Save(ctx, sess1)
	assert.Nil(t, err)
	setModTime("111")

	now = now.Add(-2 * time.Second)
	sess2, err := store.Read(ctx, "222")
	assert.Nil(t, err)

	sess2.Set("name", "flamego")
	err = store.Save(ctx, sess2)
	assert.Nil(t, err)
	setModTime("222")

	// Read on an expired session should wipe data but preserve the record
	now = now.Add(2 * time.Second)
	tmp, err := store.Read(ctx, "222")
	assert.Nil(t, err)
	assert.Nil(t, tmp.Get("name"))

	now = now.Add(-2 * time.Second)
	sess3, err := store.Read(ctx, "333")
	assert.Nil(t, err)
	err = store.Save(ctx, sess3)
	assert.Nil(t, err)
	setModTime("333")

	now = now.Add(2 * time.Second)
	err = store.GC(ctx) // sess3 should be recycled
	assert.Nil(t, err)

	assert.True(t, store.Exist(ctx, "111"))
	assert.False(t, store.Exist(ctx, "222"))
	assert.False(t, store.Exist(ctx, "333"))
}
