// Copyright 2021 Flamego. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package redis

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"

	"github.com/flamego/flamego"

	"github.com/flamego/session"
)

func newTestClient(t *testing.T, ctx context.Context) (testClient *redis.Client, cleanup func() error) {
	const db = 15
	testClient = redis.NewClient(
		&redis.Options{
			Addr: os.ExpandEnv("$REDIS_HOST:$REDIS_PORT"),
			DB:   db,
		},
	)

	err := testClient.FlushDB(ctx).Err()
	if err != nil {
		t.Fatalf("Failed to flush test database: %v", err)
	}

	t.Cleanup(func() {
		defer func() { _ = testClient.Close() }()

		if t.Failed() {
			t.Logf("DATABASE %d left intact for inspection", db)
			return
		}

		err := testClient.FlushDB(ctx).Err()
		if err != nil {
			t.Fatalf("Failed to flush test database: %v", err)
		}
	})
	return testClient, func() error {
		if t.Failed() {
			return nil
		}

		err := testClient.FlushDB(ctx).Err()
		if err != nil {
			return err
		}
		return nil
	}
}

func TestRedisStore(t *testing.T) {
	ctx := context.Background()
	client, cleanup := newTestClient(t, ctx)
	t.Cleanup(func() {
		assert.Nil(t, cleanup())
	})

	f := flamego.NewWithLogger(&bytes.Buffer{})
	f.Use(session.Sessioner(
		session.Options{
			Initer: Initer(),
			Config: Config{
				client: client,
			},
		},
	))

	now := time.Now().Truncate(0) // Truncate(0) strips monotonic clock readings.

	f.Get("/set", func(s session.Session) {
		s.Set("username", "flamego")
		s.Set("time", now)
	})
	f.Get("/get", func(s session.Session) {
		sid := s.ID()
		assert.Len(t, sid, 16)

		username, ok := s.Get("username").(string)
		assert.True(t, ok)
		assert.Equal(t, "flamego", username)

		time, ok := s.Get("time").(time.Time)
		assert.True(t, ok)
		assert.Equal(t, now, time)

		s.Delete("username")
		_, ok = s.Get("username").(string)
		assert.False(t, ok)

		s.Set("random", "value")
		s.Flush()
		_, ok = s.Get("random").(string)
		assert.False(t, ok)
	})
	f.Get("/destroy", func(c flamego.Context, session session.Session, store session.Store) error {
		return store.Destroy(c.Request().Context(), session.ID())
	})

	resp := httptest.NewRecorder()
	req, err := http.NewRequest(http.MethodGet, "/set", nil)
	assert.Nil(t, err)

	f.ServeHTTP(resp, req)
	assert.Equal(t, http.StatusOK, resp.Code)

	cookie := resp.Header().Get("Set-Cookie")

	resp = httptest.NewRecorder()
	req, err = http.NewRequest(http.MethodGet, "/get", nil)
	assert.Nil(t, err)

	req.Header.Set("Cookie", cookie)
	f.ServeHTTP(resp, req)
	assert.Equal(t, http.StatusOK, resp.Code)

	resp = httptest.NewRecorder()
	req, err = http.NewRequest(http.MethodGet, "/destroy", nil)
	assert.Nil(t, err)

	req.Header.Set("Cookie", cookie)
	f.ServeHTTP(resp, req)
	assert.Equal(t, http.StatusOK, resp.Code)
}

func TestRedisStore_GC(t *testing.T) {
	ctx := context.Background()
	client, cleanup := newTestClient(t, ctx)
	t.Cleanup(func() {
		assert.Nil(t, cleanup())
	})

	store, err := Initer()(ctx,
		Config{
			client:   client,
			Lifetime: time.Second,
		},
	)
	assert.Nil(t, err)

	sess1, err := store.Read(ctx, "1")
	assert.Nil(t, err)
	err = store.Save(ctx, sess1)
	assert.Nil(t, err)

	// NOTE: Redis is behaving flaky on exact the seconds in CI, so let's wait 100ms
	// more.
	time.Sleep(1100 * time.Millisecond)
	assert.False(t, store.Exist(ctx, "1"))

	sess2, err := store.Read(ctx, "2")
	assert.Nil(t, err)

	sess2.Set("name", "flamego")
	err = store.Save(ctx, sess2)
	assert.Nil(t, err)

	tmp, err := store.Read(ctx, "2")
	assert.Nil(t, err)
	assert.Equal(t, "flamego", tmp.Get("name"))
}
