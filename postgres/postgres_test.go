// Copyright 2021 Flamego. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package postgres

import (
	"bytes"
	"context"
	"database/sql"
	"flag"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/log/testingadapter"
	"github.com/jackc/pgx/v4/stdlib"
	"github.com/stretchr/testify/assert"

	"github.com/flamego/flamego"
	"github.com/flamego/session"
)

var flagParseOnce sync.Once

// todo: NewTestDB 初始化测试数据库连接并对表结构进行自动迁移。
// 数据库连接字符串从以下环境变量获取：`PGUSER`、`PGPASSWORD`、`PGHOST`、`PGPORT`、`PGSSLMODE`。
// 每次调用都会创建随机名称的数据库实例，并返回对应实例的数据库连接。
// 返回的 cleanup 函数可以用于重置指定数据表的数据但不销毁测试数据库，常用于子测试中。
func newTestDB(t *testing.T, ctx context.Context) (testDB *sql.DB, cleanup func() error) {
	dsn := os.ExpandEnv("postgres://$PGUSER:$PGPASSWORD@$PGHOST:$PGPORT/?sslmode=$PGSSLMODE")
	db, err := openDB(dsn)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}

	quoteIdentifier := func(s string) string {
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}

	dbname := "flamego-test-session"
	_, err = db.ExecContext(ctx, `DROP DATABASE IF EXISTS `+quoteIdentifier(dbname))
	if err != nil {
		t.Fatalf("Failed to drop test database: %v", err)
	}

	_, err = db.ExecContext(ctx, `CREATE DATABASE `+quoteIdentifier(dbname))
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}

	cfg, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("Failed to parse DSN: %v", err)
	}
	cfg.Path = "/" + dbname

	flagParseOnce.Do(flag.Parse)

	connConfig, err := pgx.ParseConfig(cfg.String())
	if err != nil {
		t.Fatalf("Failed to parse test database config: %v", err)
	}
	if testing.Verbose() {
		connConfig.Logger = testingadapter.NewLogger(t)
		connConfig.LogLevel = pgx.LogLevelTrace
	}

	testDB = stdlib.OpenDB(*connConfig)

	q := `
CREATE TABLE sessions (
    key        TEXT PRIMARY KEY,
    data       BYTEA NOT NULL,
    expired_at TIMESTAMP WITH TIME ZONE NOT NULL
)`
	_, err = testDB.ExecContext(ctx, q)
	if err != nil {
		t.Fatalf("Failed to create sessions table: %v", err)
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

		_, err = db.ExecContext(ctx, `DROP DATABASE `+quoteIdentifier(dbname))
		if err != nil {
			t.Fatalf("Failed to drop test database: %v", err)
		}
	})
	return testDB, func() error {
		if t.Failed() {
			return nil
		}

		_, err = testDB.ExecContext(ctx, `TRUNCATE `+quoteIdentifier("sessions")+` RESTART IDENTITY CASCADE`)
		if err != nil {
			return err
		}
		return nil
	}
}

func TestPostgresStore(t *testing.T) {
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

func TestPostgresStore_GC(t *testing.T) {
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
	assert.Nil(t, err)

	_, err = store.Read(ctx, "1")
	assert.Nil(t, err)

	now = now.Add(-2 * time.Second)
	sess2, err := store.Read(ctx, "2")
	assert.Nil(t, err)

	sess2.Set("name", "flamego")
	err = store.Save(ctx, sess2)
	assert.Nil(t, err)

	// Read on an expired session should wipe data but preserve the record
	now = now.Add(2 * time.Second)
	tmp, err := store.Read(ctx, "2")
	assert.Nil(t, err)
	assert.Nil(t, tmp.Get("name"))

	now = now.Add(-2 * time.Second)
	_, err = store.Read(ctx, "3")
	assert.Nil(t, err)

	now = now.Add(2 * time.Second)
	err = store.GC(ctx) // sess3 should be recycled
	assert.Nil(t, err)

	assert.True(t, store.Exist(ctx, "1"))
	assert.False(t, store.Exist(ctx, "2"))
	assert.False(t, store.Exist(ctx, "3"))
}
