// Copyright 2021 Flamego. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v4"
	"github.com/pkg/errors"

	"github.com/flamego/session"
)

var _ session.Store = (*postgresStore)(nil)

// postgres}Store is an in-memory implementation of the session store.
type postgresStore struct {
	nowFunc  func() time.Time // The function to return the current time
	lifetime time.Duration    // The duration to have no access to a session before being recycled
	conn     *pgx.Conn        // The database connection
	table    string           // The database table for storing session data
	encoder  session.Encoder  // The encoder to encode the session data before saving
	decoder  session.Decoder  // The decoder to decode binary to session data after reading
}

// newPostgresStore returns a new Postgres session store based on given
// configuration.
func newPostgresStore(cfg Config, conn *pgx.Conn) *postgresStore {
	return &postgresStore{
		nowFunc:  cfg.nowFunc,
		lifetime: cfg.Lifetime,
		conn:     conn,
		table:    cfg.Table,
		encoder:  cfg.Encoder,
		decoder:  cfg.Decoder,
	}
}

func (s *postgresStore) Exist(ctx context.Context, sid string) bool {
	var exists bool
	sql := fmt.Sprintf(`SELECT EXISTS (SELECT 1 FROM %s WHERE key = $1)`, s.table)
	err := s.conn.QueryRow(ctx, sql, sid).Scan(&exists)
	return err == nil && exists
}

func (s *postgresStore) Read(ctx context.Context, sid string) (session.Session, error) {
	var binary []byte
	var expiredAt time.Time
	sql := fmt.Sprintf(`SELECT data, expired_at FROM %s WHERE key = $1`, s.table)
	err := s.conn.QueryRow(ctx, sql, sid).Scan(&binary, &expiredAt)
	if err == nil {
		data, err := s.decoder(binary)
		if err != nil {
			return nil, errors.Wrap(err, "decode")
		}

		sess := session.NewBaseSession(sid, s.encoder)
		sess.SetData(data)
		return sess, nil
	} else if err != pgx.ErrNoRows {
		return nil, errors.Wrap(err, "select")
	}

	binary, err = s.encoder(make(session.Data))
	if err != nil {
		return nil, errors.Wrap(err, "encode")
	}

	sql = fmt.Sprintf(`INSERT INTO %s (key, data, expired_at) VALUES ($1, $2, $3)`, s.table)
	_, err = s.conn.Exec(ctx, sql, sid, binary, s.nowFunc().Add(s.lifetime).UTC())
	if err != nil {
		return nil, errors.Wrap(err, "insert")
	}
	return session.NewBaseSession(sid, s.encoder), nil
}

func (s *postgresStore) Destroy(ctx context.Context, sid string) error {
	sql := fmt.Sprintf(`DELETE FROM %s WHERE key = $1`, s.table)
	_, err := s.conn.Exec(ctx, sql, sid)
	return err
}

func (s *postgresStore) Save(ctx context.Context, sess session.Session) error {
	binary, err := sess.Encode()
	if err != nil {
		return errors.Wrap(err, "encode")
	}

	sql := fmt.Sprintf(`UPDATE %s SET data = $1, expired_at = $2 WHERE key = $3`, s.table)
	_, err = s.conn.Exec(ctx, sql, binary, s.nowFunc().Add(s.lifetime).UTC(), sess.ID())
	if err != nil {
		return errors.Wrap(err, "update")
	}
	return nil
}

func (s *postgresStore) GC(ctx context.Context) error {
	sql := fmt.Sprintf(`DELETE FROM %s WHERE expired_at <= $1`, s.table)
	_, err := s.conn.Exec(ctx, sql, s.nowFunc().UTC())
	return err
}

// Config contains options for the memory session store.
type Config struct {
	nowFunc func() time.Time // For tests only

	// Lifetime is the duration to have no access to a session before being
	// recycled. Default is 3600 seconds.
	Lifetime time.Duration
	// ConnString is the connection string to the Postgres.
	ConnString string
	// Table is the table name for storing session data. Default is "sessions".
	Table string
	// Encoder is the encoder to encode session data. Default is session.GobEncoder.
	Encoder session.Encoder
	// Decoder is the decoder to decode session data. Default is session.GobDecoder.
	Decoder session.Decoder
}

// Initer returns the session.Initer for the Postgres session store.
func Initer() session.Initer {
	return func(args ...interface{}) (session.Store, error) {
		var ctx context.Context
		var cfg *Config
		for i := range args {
			switch v := args[i].(type) {
			case context.Context:
				ctx = v
			case Config:
				cfg = &v
			}
		}

		if ctx == nil {
			ctx = context.Background()
		}

		if cfg == nil {
			return nil, fmt.Errorf("config object with the type '%T' not found", Config{})
		} else if cfg.ConnString == "" {
			return nil, errors.New("empty ConnString")
		}

		conn, err := pgx.Connect(ctx, cfg.ConnString)
		if err != nil {
			return nil, errors.Wrap(err, "connect")
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

		return newPostgresStore(*cfg, conn), nil
	}
}
