// Copyright 2021 Flamego. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package session

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/pkg/errors"
)

var _ Store = (*fileStore)(nil)

// fileStore is a file implementation of the session store.
type fileStore struct {
	nowFunc  func() time.Time // The function to return the current time
	lifetime time.Duration    // The duration to have no access to a session before being recycled
	rootDir  string           // The root directory of file session items stored on the local file system
	encoder  Encoder          // The encoder to encode the session data before saving
	decoder  Decoder          // The decoder to decode binary to session data after reading
}

// newFileStore returns a new file session store based on given configuration.
func newFileStore(cfg FileConfig) *fileStore {
	return &fileStore{
		nowFunc:  cfg.nowFunc,
		lifetime: cfg.Lifetime,
		rootDir:  cfg.RootDir,
		encoder:  cfg.Encoder,
		decoder:  cfg.Decoder,
	}
}

// filename returns the computed file name with given sid.
func (s *fileStore) filename(sid string) string {
	return filepath.Join(s.rootDir, string(sid[0]), string(sid[1]), sid)
}

// isFile returns true if given path exists as a file (i.e. not a directory).
func isFile(path string) bool {
	f, e := os.Stat(path)
	if e != nil {
		return false
	}
	return !f.IsDir()
}

func (s *fileStore) Exist(_ context.Context, sid string) bool {
	if len(sid) < minimumSIDLength {
		return false
	}
	return isFile(s.filename(sid))
}

func (s *fileStore) Read(_ context.Context, sid string) (Session, error) {
	if len(sid) < minimumSIDLength {
		return nil, ErrMinimumSIDLength
	}

	filename := s.filename(sid)
	if !isFile(filename) {
		err := os.MkdirAll(filepath.Dir(filename), 0700)
		if err != nil {
			return nil, errors.Wrap(err, "create parent directory")
		}

		return NewBaseSession(sid, s.encoder), nil
	}

	// Discard existing data if it's expired
	fi, err := os.Stat(filename)
	if err != nil {
		return nil, errors.Wrap(err, "stat file")
	}
	if !fi.ModTime().Add(s.lifetime).After(s.nowFunc()) {
		return NewBaseSession(sid, s.encoder), nil
	}

	binary, err := os.ReadFile(filename)
	if err != nil {
		return nil, errors.Wrap(err, "read file")
	}

	data, err := s.decoder(binary)
	if err != nil {
		return nil, errors.Wrap(err, "decode")
	}
	return NewBaseSessionWithData(sid, s.encoder, data), nil
}

func (s *fileStore) Destroy(_ context.Context, sid string) error {
	if len(sid) < minimumSIDLength {
		return nil
	}
	return os.Remove(s.filename(sid))
}

func (s *fileStore) Touch(_ context.Context, sid string) error {
	filename := s.filename(sid)
	if !isFile(filename) {
		return nil
	}

	err := os.Chtimes(filename, s.nowFunc(), s.nowFunc())
	if err != nil {
		return errors.Wrap(err, "change times")
	}
	return nil
}

func (s *fileStore) Save(_ context.Context, sess Session) error {
	if len(sess.ID()) < minimumSIDLength {
		return ErrMinimumSIDLength
	}

	binary, err := sess.Encode()
	if err != nil {
		return errors.Wrap(err, "encode")
	}

	filename := s.filename(sess.ID())
	err = os.WriteFile(filename, binary, 0600)
	if err != nil {
		return errors.Wrap(err, "write file")
	}

	err = os.Chtimes(filename, s.nowFunc(), s.nowFunc())
	if err != nil {
		return errors.Wrap(err, "change times")
	}
	return nil
}

func (s *fileStore) GC(ctx context.Context) error {
	err := filepath.WalkDir(s.rootDir, func(path string, d fs.DirEntry, err error) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		fi, err := d.Info()
		if err != nil {
			return err
		}
		if fi.ModTime().Add(s.lifetime).After(s.nowFunc()) {
			return nil
		}
		return os.Remove(path)
	})
	if err != nil && !errors.Is(err, ctx.Err()) {
		return err
	}
	return nil
}

// FileConfig contains options for the file session store.
type FileConfig struct {
	// For tests only
	nowFunc func() time.Time

	// Lifetime is the duration to have no access to a session before being
	// recycled. Default is 3600 seconds.
	Lifetime time.Duration
	// RootDir is the root directory of file session items stored on the local file
	// system. Default is "sessions".
	RootDir string
	// Encoder is the encoder to encode session data. Default is GobEncoder.
	Encoder Encoder
	// Decoder is the decoder to decode session data. Default is GobDecoder.
	Decoder Decoder
}

// FileIniter returns the Initer for the file session store.
func FileIniter() Initer {
	return func(ctx context.Context, args ...interface{}) (Store, error) {
		var cfg *FileConfig
		for i := range args {
			switch v := args[i].(type) {
			case FileConfig:
				cfg = &v
			}
		}

		if cfg == nil {
			return nil, fmt.Errorf("config object with the type '%T' not found", FileConfig{})
		}
		if cfg.nowFunc == nil {
			cfg.nowFunc = time.Now
		}
		if cfg.Lifetime.Seconds() < 1 {
			cfg.Lifetime = 3600 * time.Second
		}
		if cfg.RootDir == "" {
			cfg.RootDir = "sessions"
		}
		if cfg.Encoder == nil {
			cfg.Encoder = GobEncoder
		}
		if cfg.Decoder == nil {
			cfg.Decoder = GobDecoder
		}

		return newFileStore(*cfg), nil
	}
}
