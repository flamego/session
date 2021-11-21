// Copyright 2021 Flamego. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/pkg/errors"

	"github.com/flamego/session"
)

var _ session.Store = (*redisStore)(nil)

// redisStore is a Redis implementation of the session store.
type redisStore struct {
	client   *redis.Client   // The client connection
	lifetime time.Duration   // The duration to have access to a session before being recycled
	encoder  session.Encoder // The encoder to encode the session data before saving
	decoder  session.Decoder // The decoder to decode binary to session data after reading
}

// newRedisStore returns a new Redis session store based on given configuration.
func newRedisStore(cfg Config) *redisStore {
	return &redisStore{
		client:   cfg.client,
		lifetime: cfg.Lifetime,
		encoder:  cfg.Encoder,
		decoder:  cfg.Decoder,
	}
}

func (s *redisStore) Exist(ctx context.Context, sid string) bool {
	result, err := s.client.Exists(ctx, sid).Result()
	return err == nil && result == 1
}

func (s *redisStore) Read(ctx context.Context, sid string) (session.Session, error) {
	binary, err := s.client.Get(ctx, sid).Result()
	if err != nil {
		if err == redis.Nil {
			return session.NewBaseSession(sid, s.encoder), nil
		}
		return nil, errors.Wrap(err, "get")
	}

	data, err := s.decoder([]byte(binary))
	if err != nil {
		return nil, errors.Wrap(err, "decode")
	}

	sess := session.NewBaseSession(sid, s.encoder)
	sess.SetData(data)
	return sess, nil
}

func (s *redisStore) Destroy(ctx context.Context, sid string) error {
	return s.client.Del(ctx, sid).Err()
}

func (s *redisStore) Save(ctx context.Context, sess session.Session) error {
	binary, err := sess.Encode()
	if err != nil {
		return errors.Wrap(err, "encode")
	}

	err = s.client.SetEX(ctx, sess.ID(), binary, s.lifetime).Err()
	if err != nil {
		return errors.Wrap(err, "set")
	}
	return nil
}

func (s *redisStore) GC(_ context.Context) error {
	return nil
}

// Options keeps the settings to set up Redis client connection.
type Options = redis.Options

// Config contains options for the Redis session store.
type Config struct {
	// For tests only
	client *redis.Client

	// Options is the settings to set up Redis client connection.
	Options *Options
	// Lifetime is the duration to have no access to a session before being
	// recycled. Default is 3600 seconds.
	Lifetime time.Duration
	// Encoder is the encoder to encode session data. Default is session.GobEncoder.
	Encoder session.Encoder
	// Decoder is the decoder to decode session data. Default is session.GobDecoder.
	Decoder session.Decoder
}

// Initer returns the session.Initer for the Redis session store.
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
		} else if cfg.Options == nil && cfg.client == nil {
			return nil, errors.New("empty Options")
		}

		if cfg.client == nil {
			cfg.client = redis.NewClient(cfg.Options)
		}

		if cfg.Lifetime.Seconds() < 1 {
			cfg.Lifetime = 3600 * time.Second
		}
		if cfg.Encoder == nil {
			cfg.Encoder = session.GobEncoder
		}
		if cfg.Decoder == nil {
			cfg.Decoder = session.GobDecoder
		}

		return newRedisStore(*cfg), nil
	}
}
