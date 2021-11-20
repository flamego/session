// Copyright 2021 Flamego. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package mongo

import (
	"context"
	"fmt"
	"time"

	"github.com/pkg/errors"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/flamego/session"
)

var _ session.Store = (*mongoStore)(nil)

// mongoStore is a MongoDB implementation of the session store.
type mongoStore struct {
	nowFunc    func() time.Time // The function to return the current time
	lifetime   time.Duration    // The duration to have no access to a session before being recycled
	db         *mongo.Database  // The database connection
	collection string           // The database collection for storing session data
	encoder    session.Encoder  // The encoder to encode the session data before saving
	decoder    session.Decoder  // The decoder to decode binary to session data after reading
}

// newMongoStore returns a new MongoDB session store based on given configuration.
func newMongoStore(cfg Config) *mongoStore {
	return &mongoStore{
		nowFunc:    cfg.nowFunc,
		lifetime:   cfg.Lifetime,
		db:         cfg.db,
		collection: cfg.Collection,
		encoder:    cfg.Encoder,
		decoder:    cfg.Decoder,
	}
}

func (s mongoStore) Exist(ctx context.Context, sid string) bool {
	err := s.db.Collection(s.collection).FindOne(ctx, bson.M{"key": sid}).Err()
	if err == mongo.ErrNoDocuments {
		return false
	}
	return true
}

func (s mongoStore) Read(ctx context.Context, sid string) (session.Session, error) {
	var result bson.M
	err := s.db.Collection(s.collection).FindOne(ctx, bson.M{"key": sid}).Decode(&result)
	if err == nil {
		binary, ok := result["data"].(primitive.Binary)
		if !ok {
			return nil, errors.New("assert `data` key")
		}

		expiredAt, ok := result["expired_at"].(primitive.DateTime)
		if !ok {
			return nil, errors.New("assert `expired_at` key")
		}

		// Discard existing data if it's expired
		if !s.nowFunc().Before(expiredAt.Time().Add(s.lifetime)) {
			return session.NewBaseSession(sid, s.encoder), nil
		}

		data, err := s.decoder(binary.Data)
		if err != nil {
			return nil, errors.Wrap(err, "decode")
		}

		sess := session.NewBaseSession(sid, s.encoder)
		sess.SetData(data)
		return sess, nil
	} else if err != mongo.ErrNoDocuments {
		return nil, errors.Wrap(err, "select")
	}

	return session.NewBaseSession(sid, s.encoder), nil

}

func (s mongoStore) Destroy(ctx context.Context, sid string) error {
	_, err := s.db.Collection(s.collection).DeleteOne(ctx, bson.M{"key": sid})
	if err != nil {
		return errors.Wrap(err, "delete")
	}
	return nil
}

func (s mongoStore) Save(ctx context.Context, sess session.Session) error {
	binary, err := sess.Encode()
	if err != nil {
		return errors.Wrap(err, "encode")
	}

	upsert := true
	_, err = s.db.Collection(s.collection).
		UpdateOne(ctx, bson.M{"key": sess.ID()}, bson.M{"$set": bson.M{
			"key":        sess.ID(),
			"data":       binary,
			"expired_at": s.nowFunc().Add(s.lifetime).UTC(),
		}}, &options.UpdateOptions{
			Upsert: &upsert,
		})
	if err != nil {
		return errors.Wrap(err, "upsert")
	}
	return nil
}

func (s mongoStore) GC(ctx context.Context) error {
	_, err := s.db.Collection(s.collection).DeleteMany(ctx, bson.M{"expired_at": bson.M{"$lt": s.nowFunc().UTC()}})
	if err != nil {
		return errors.Wrap(err, "GC")
	}
	return nil
}

// Options keeps the settings to set up Redis client connection.
type Options = options.ClientOptions

// Config contains options for the Redis session store.
type Config struct {
	// For tests only
	nowFunc func() time.Time
	db      *mongo.Database

	// Options is the settings to set up MongoDB client connection.
	Options *Options
	// DSN is the database source name to the MongoDB.
	DSN string
	// Collection is the collection name for storing session data. Default is "sessions".
	Collection string
	// Lifetime is the duration to have no access to a session before being
	// recycled. Default is 3600 seconds.
	Lifetime time.Duration
	// Encoder is the encoder to encode session data. Default is bson.Encoder.
	Encoder session.Encoder
	// Decoder is the decoder to decode session data. Default is bson.Decoder.
	Decoder session.Decoder
}

// Initer returns the session.Initer for the MongoDB session store.
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
			client, err := mongo.Connect(ctx, cfg.Options)
			if err != nil {
				return nil, errors.Wrap(err, "open database")
			}
			cfg.db = client.Database(cfg.DSN)
		}

		if cfg.nowFunc == nil {
			cfg.nowFunc = time.Now
		}
		if cfg.Lifetime.Seconds() < 1 {
			cfg.Lifetime = 3600 * time.Second
		}
		if cfg.Collection == "" {
			cfg.Collection = "sessions"
		}
		if cfg.Encoder == nil {
			cfg.Encoder = session.GobEncoder
		}
		if cfg.Decoder == nil {
			cfg.Decoder = session.GobDecoder
		}

		return newMongoStore(*cfg), nil
	}
}
