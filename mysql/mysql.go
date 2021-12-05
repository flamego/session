// Copyright 2021 Flamego. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/pkg/errors"

	"github.com/flamego/session"
)

var _ session.Store = (*mysqlStore)(nil)

// mysqlStore is a MySQL implementation of the session store.
type mysqlStore struct {
	nowFunc  func() time.Time // The function to return the current time
	lifetime time.Duration    // The duration to have no access to a session before being recycled
	db       *sql.DB          // The database connection
	table    string           // The database table for storing session data
	encoder  session.Encoder  // The encoder to encode the session data before saving
	decoder  session.Decoder  // The decoder to decode binary to session data after reading
}

// newMySQLStore returns a new MySQL session store based on given configuration.
func newMySQLStore(cfg Config) *mysqlStore {
	return &mysqlStore{
		nowFunc:  cfg.nowFunc,
		lifetime: cfg.Lifetime,
		db:       cfg.db,
		table:    cfg.Table,
		encoder:  cfg.Encoder,
		decoder:  cfg.Decoder,
	}
}

func quoteWithBackticks(s string) string {
	return "`" + s + "`"
}

func (s *mysqlStore) Exist(ctx context.Context, sid string) bool {
	var exists bool
	q := fmt.Sprintf(
		`SELECT EXISTS (SELECT 1 FROM %s WHERE %s = ?)`,
		quoteWithBackticks(s.table),
		quoteWithBackticks("key"),
	)
	err := s.db.QueryRowContext(ctx, q, sid).Scan(&exists)
	return err == nil && exists
}

func (s *mysqlStore) Read(ctx context.Context, sid string) (session.Session, error) {
	var binary []byte
	var expiredAt time.Time
	q := fmt.Sprintf(
		`SELECT data, expired_at FROM %s WHERE %s = ?`,
		quoteWithBackticks(s.table),
		quoteWithBackticks("key"),
	)
	err := s.db.QueryRowContext(ctx, q, sid).Scan(&binary, &expiredAt)
	if err == nil {
		// Discard existing data if it's expired
		if !s.nowFunc().Before(expiredAt.Add(s.lifetime)) {
			return session.NewBaseSession(sid, s.encoder), nil
		}

		data, err := s.decoder(binary)
		if err != nil {
			return nil, errors.Wrap(err, "decode")
		}

		sess := session.NewBaseSession(sid, s.encoder)
		sess.SetData(data)
		return sess, nil
	} else if err != sql.ErrNoRows {
		return nil, errors.Wrap(err, "select")
	}

	return session.NewBaseSession(sid, s.encoder), nil
}

func (s *mysqlStore) Destroy(ctx context.Context, sid string) error {
	q := fmt.Sprintf(
		`DELETE FROM %s WHERE %s = ?`,
		quoteWithBackticks(s.table),
		quoteWithBackticks("key"),
	)
	_, err := s.db.ExecContext(ctx, q, sid)
	return err
}

func (s *mysqlStore) Save(ctx context.Context, sess session.Session) error {
	binary, err := sess.Encode()
	if err != nil {
		return errors.Wrap(err, "encode")
	}

	q := fmt.Sprintf(`
INSERT INTO %s (%s, data, expired_at)
VALUES (?, ?, ?)
ON DUPLICATE KEY UPDATE
	data       = VALUES(data),
	expired_at = VALUES(expired_at)
`,
		quoteWithBackticks(s.table),
		quoteWithBackticks("key"),
	)
	_, err = s.db.ExecContext(ctx, q, sess.ID(), binary, s.nowFunc().Add(s.lifetime).UTC())
	if err != nil {
		return errors.Wrap(err, "upsert")
	}
	return nil
}

func (s *mysqlStore) GC(ctx context.Context) error {
	q := fmt.Sprintf(`DELETE FROM %s WHERE expired_at <= ?`, quoteWithBackticks(s.table))
	_, err := s.db.ExecContext(ctx, q, s.nowFunc().UTC())
	return err
}

// Config contains options for the MySQL session store.
type Config struct {
	// For tests only
	nowFunc func() time.Time
	db      *sql.DB

	// Lifetime is the duration to have access to a session before being
	// recycled. Default is 3600 seconds.
	Lifetime time.Duration
	// DSN is the database source name to the MySQL.
	DSN string
	// Table is the table name for storing session data. Default is "sessions".
	Table string
	// Encoder is the encoder to encode session data. Default is session.GobEncoder.
	Encoder session.Encoder
	// Decoder is the decoder to decode session data. Default is session.GobDecoder.
	Decoder session.Decoder
	// InitTable indicates whether to create a default session table when not exists automatically.
	InitTable bool
}

// Initer returns the session.Initer for the MySQL session store.
func Initer() session.Initer {
	return func(ctx context.Context, args ...interface{}) (session.Store, error) {
		var cfg *Config
		for i := range args {
			switch v := args[i].(type) {
			case Config:
				cfg = &v
			}
		}

		if cfg == nil {
			return nil, fmt.Errorf("config object with the type '%T' not found", Config{})
		} else if cfg.DSN == "" && cfg.db == nil {
			return nil, errors.New("empty DSN")
		}

		if cfg.db == nil {
			db, err := sql.Open("mysql", cfg.DSN)
			if err != nil {
				return nil, errors.Wrap(err, "open database")
			}
			cfg.db = db
		}

		if cfg.InitTable {
			q := fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS sessions (
	%[1]s      VARCHAR(255) NOT NULL,
	data       BLOB NOT NULL,
	expired_at DATETIME NOT NULL,
	PRIMARY KEY (%[1]s)
) DEFAULT CHARSET=utf8`,
				quoteWithBackticks("key"),
			)

			_, err := cfg.db.ExecContext(ctx, q)
			if err != nil {
				return nil, errors.Wrap(err, "create table")
			}
		}

		if cfg.nowFunc == nil {
			cfg.nowFunc = time.Now
		}
		if cfg.Lifetime.Seconds() < 1 {
			cfg.Lifetime = 3600 * time.Second
		}
		if cfg.Table == "" {
			cfg.Table = "sessions"
		}
		if cfg.Encoder == nil {
			cfg.Encoder = session.GobEncoder
		}
		if cfg.Decoder == nil {
			cfg.Decoder = session.GobDecoder
		}

		return newMySQLStore(*cfg), nil
	}
}
