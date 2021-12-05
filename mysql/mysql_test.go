// Copyright 2021 Flamego. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package mysql

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"

	"github.com/flamego/flamego"
	"github.com/flamego/session"
)

func newTestDB(t *testing.T, ctx context.Context) (testDB *sql.DB, cleanup func() error) {
	dsn := os.ExpandEnv("$MYSQL_USER:$MYSQL_PASSWORD@tcp($MYSQL_HOST:$MYSQL_PORT)/?charset=utf8&parseTime=true")
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}

	dbname := "flamego-test-sessions"
	_, err = db.ExecContext(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", quoteWithBackticks(dbname)))
	if err != nil {
		t.Fatalf("Failed to drop test database: %v", err)
	}

	_, err = db.ExecContext(ctx, fmt.Sprintf("CREATE DATABASE %s", quoteWithBackticks(dbname)))
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}

	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("Failed to parse DSN: %v", err)
	}
	cfg.DBName = dbname

	testDB, err = sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}

	t.Cleanup(func() {
		defer func() { _ = db.Close() }()

		if t.Failed() {
			t.Logf("DATABASE %s left intact for inspection", dbname)
			return
		}

		err := testDB.Close()
		if err != nil {
			t.Fatalf("Failed to close test connection: %v", err)
		}

		_, err = db.ExecContext(ctx, fmt.Sprintf(`DROP DATABASE %s`, quoteWithBackticks(dbname)))
		if err != nil {
			t.Fatalf("Failed to drop test database: %v", err)
		}
	})
	return testDB, func() error {
		if t.Failed() {
			return nil
		}

		_, err = testDB.ExecContext(ctx, `TRUNCATE TABLE sessions`)
		if err != nil {
			return err
		}
		return nil
	}
}

func TestMySQLStore(t *testing.T) {
	ctx := context.Background()
	db, cleanup := newTestDB(t, ctx)
	t.Cleanup(func() {
		assert.Nil(t, cleanup())
	})

	f := flamego.NewWithLogger(&bytes.Buffer{})
	f.Use(session.Sessioner(
		session.Options{
			Initer: Initer(),
			Config: Config{
				nowFunc:   time.Now,
				db:        db,
				InitTable: true,
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

func TestMySQLStore_GC(t *testing.T) {
	ctx := context.Background()
	db, cleanup := newTestDB(t, ctx)
	t.Cleanup(func() {
		assert.Nil(t, cleanup())
	})

	now := time.Now()
	store, err := Initer()(ctx,
		Config{
			nowFunc:   func() time.Time { return now },
			db:        db,
			Lifetime:  time.Second,
			InitTable: true,
		},
	)
	assert.Nil(t, err)

	now = now.Add(3 * time.Second)
	sess1, err := store.Read(ctx, "1")
	assert.Nil(t, err)
	err = store.Save(ctx, sess1)
	assert.Nil(t, err)
	now = now.Add(-3 * time.Second)

	now = now.Add(-2 * time.Second)
	sess2, err := store.Read(ctx, "2")
	assert.Nil(t, err)

	sess2.Set("name", "flamego")
	err = store.Save(ctx, sess2)
	assert.Nil(t, err)

	// Read on an expired session should wipe data but preserve the record.
	// NOTE: MySQL is behaving flaky on exact the seconds, so let's wait one more
	//  second.
	now = now.Add(3 * time.Second)
	tmp, err := store.Read(ctx, "2")
	assert.Nil(t, err)
	assert.Nil(t, tmp.Get("name"))

	now = now.Add(-2 * time.Second)
	sess3, err := store.Read(ctx, "3")
	assert.Nil(t, err)
	err = store.Save(ctx, sess3)
	assert.Nil(t, err)

	now = now.Add(3 * time.Second)
	err = store.GC(ctx) // sess3 should be recycled
	assert.Nil(t, err)

	assert.True(t, store.Exist(ctx, "1"))
	assert.False(t, store.Exist(ctx, "2"))
	assert.False(t, store.Exist(ctx, "3"))
}
