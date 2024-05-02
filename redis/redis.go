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
	client    *redis.Client // The client connection
	keyPrefix string        // The prefix to use for keys
	lifetime  time.Duration // The duration to have access to a session before being recycled

	encoder  session.Encoder
	decoder  session.Decoder
	idWriter session.IDWriter
}

// newRedisStore returns a new Redis session store based on given configuration.
func newRedisStore(cfg Config, idWriter session.IDWriter) *redisStore {
	return &redisStore{
		client:    cfg.Client,
		keyPrefix: cfg.KeyPrefix,
		lifetime:  cfg.Lifetime,
		encoder:   cfg.Encoder,
		decoder:   cfg.Decoder,
		idWriter:  idWriter,
	}
}

func (s *redisStore) Exist(ctx context.Context, sid string) bool {
	result, err := s.client.Exists(ctx, s.keyPrefix+sid).Result()
	return err == nil && result == 1
}

func (s *redisStore) Read(ctx context.Context, sid string) (session.Session, error) {
	binary, err := s.client.Get(ctx, s.keyPrefix+sid).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return session.NewBaseSession(sid, s.encoder, s.idWriter), nil
		}
		return nil, errors.Wrap(err, "get")
	}

	data, err := s.decoder([]byte(binary))
	if err != nil {
		return nil, errors.Wrap(err, "decode")
	}
	return session.NewBaseSessionWithData(sid, s.encoder, s.idWriter, data), nil
}

func (s *redisStore) Destroy(ctx context.Context, sid string) error {
	return s.client.Del(ctx, s.keyPrefix+sid).Err()
}

func (s *redisStore) Touch(ctx context.Context, sid string) error {
	err := s.client.Expire(ctx, s.keyPrefix+sid, s.lifetime).Err()
	if err != nil {
		return errors.Wrap(err, "expire")
	}
	return nil
}

func (s *redisStore) Save(ctx context.Context, sess session.Session) error {
	binary, err := sess.Encode()
	if err != nil {
		return errors.Wrap(err, "encode")
	}

	err = s.client.SetEX(ctx, s.keyPrefix+sess.ID(), binary, s.lifetime).Err()
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
	// Client is the Redis Client connection. If not set, a new client will be
	// created based on Options.
	Client *redis.Client
	// Options is the settings to set up Redis client connection.
	Options *Options
	// KeyPrefix is the prefix to use for keys in Redis. Default is "session:".
	KeyPrefix string
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
		var idWriter session.IDWriter
		for i := range args {
			switch v := args[i].(type) {
			case Config:
				cfg = &v
			case session.IDWriter:
				idWriter = v
			}
		}
		if idWriter == nil {
			return nil, errors.New("IDWriter not given")
		}

		if cfg == nil {
			return nil, fmt.Errorf("config object with the type '%T' not found", Config{})
		} else if cfg.Options == nil && cfg.Client == nil {
			return nil, errors.New("empty Options")
		}

		if cfg.Client == nil {
			cfg.Client = redis.NewClient(cfg.Options)
		}
		if cfg.KeyPrefix == "" {
			cfg.KeyPrefix = "session:"
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

		return newRedisStore(*cfg, idWriter), nil
	}
}
