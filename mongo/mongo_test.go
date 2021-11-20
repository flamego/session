// Copyright 2021 Flamego. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package mongo

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/flamego/flamego"
	"github.com/stretchr/testify/assert"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/flamego/session"
)

func newTestDB(t *testing.T, ctx context.Context) (testDB *mongo.Database, cleanup func() error) {
	uri := os.Getenv("MONGODB_URI")
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("Failed to connect to mongo: %v", err)
	}

	dbname := "flamego-test-sessions"
	err = client.Database(dbname).Drop(ctx)
	if err != nil {
		t.Fatalf("Failed to drop test database: %v", err)
	}
	db := client.Database(dbname)
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("DATABASE %s left intact for inspection", dbname)
			return
		}

		err = db.Drop(ctx)
		if err != nil {
			t.Fatalf("Failed to drop test database: %v", err)
		}
	})
	return db, func() error {
		if t.Failed() {
			return nil
		}

		err = db.Collection("sessions").Drop(ctx)
		if err != nil {
			return err
		}
		return nil
	}
}

func TestMongoDBStore(t *testing.T) {
	ctx := context.Background()
	db, cleanup := newTestDB(t, ctx)
	t.Cleanup(func() {
		assert.NoError(t, cleanup())
	})

	f := flamego.NewWithLogger(&bytes.Buffer{})
	f.Use(session.Sessioner(
		session.Options{
			Initer: Initer(),
			Config: Config{
				nowFunc: time.Now,
				db:      db,
			},
		},
	))

	f.Get("/set", func(s session.Session) {
		s.Set("username", "flamego")
	})
	f.Get("/get", func(s session.Session) {
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
	f.Get("/destroy", func(c flamego.Context, session session.Session, store session.Store) error {
		return store.Destroy(c.Request().Context(), session.ID())
	})

	resp := httptest.NewRecorder()
	req, err := http.NewRequest(http.MethodGet, "/set", nil)
	assert.NoError(t, err)

	f.ServeHTTP(resp, req)
	assert.Equal(t, http.StatusOK, resp.Code)

	cookie := resp.Header().Get("Set-Cookie")

	resp = httptest.NewRecorder()
	req, err = http.NewRequest(http.MethodGet, "/get", nil)
	assert.NoError(t, err)

	req.Header.Set("Cookie", cookie)
	f.ServeHTTP(resp, req)
	assert.Equal(t, http.StatusOK, resp.Code)

	resp = httptest.NewRecorder()
	req, err = http.NewRequest(http.MethodGet, "/destroy", nil)
	assert.NoError(t, err)

	req.Header.Set("Cookie", cookie)
	f.ServeHTTP(resp, req)
	assert.Equal(t, http.StatusOK, resp.Code)
}

func TestRedisStore_GC(t *testing.T) {
	ctx := context.Background()
	db, cleanup := newTestDB(t, ctx)
	t.Cleanup(func() {
		assert.Nil(t, cleanup())
	})

	now := time.Now()
	store, err := Initer()(ctx,
		Config{
			nowFunc:  func() time.Time { return now },
			db:       db,
			Lifetime: time.Second,
		},
	)
	assert.NoError(t, err)

	sess1, err := store.Read(ctx, "1")
	assert.NoError(t, err)
	err = store.Save(ctx, sess1)
	assert.NoError(t, err)

	now = now.Add(-2 * time.Second)
	sess2, err := store.Read(ctx, "2")
	assert.NoError(t, err)

	sess2.Set("name", "flamego")
	err = store.Save(ctx, sess2)
	assert.NoError(t, err)

	// Read on an expired session should wipe data but preserve the record
	now = now.Add(2 * time.Second)
	tmp, err := store.Read(ctx, "2")
	assert.NoError(t, err)
	assert.Nil(t, tmp.Get("name"))

	now = now.Add(-2 * time.Second)
	sess3, err := store.Read(ctx, "3")
	assert.NoError(t, err)
	err = store.Save(ctx, sess3)
	assert.NoError(t, err)

	now = now.Add(2 * time.Second)
	err = store.GC(ctx) // sess3 should be recycled
	assert.NoError(t, err)

	assert.True(t, store.Exist(ctx, "1"))
	assert.False(t, store.Exist(ctx, "2"))
	assert.False(t, store.Exist(ctx, "3"))
}
