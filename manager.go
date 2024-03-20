// Copyright 2021 Flamego. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package session

import (
	"context"
	"crypto/rand"
	"math/big"
	"net/http"
	"time"

	"github.com/pkg/errors"
)

// Store is a session store with capabilities of checking, reading, destroying
// and GC sessions.
type Store interface {
	// Exist returns true of the session with given ID exists.
	Exist(ctx context.Context, sid string) bool
	// Read returns the session with given ID. If a session with the ID does not
	// exist, a new session with the same ID is created and returned.
	Read(ctx context.Context, sid string) (Session, error)
	// Destroy deletes session with given ID from the session store completely.
	Destroy(ctx context.Context, sid string) error
	// Touch updates the expiry time of the session with given ID. It does nothing
	// if there is no session associated with the ID.
	Touch(ctx context.Context, sid string) error
	// Save persists session data to the session store.
	Save(ctx context.Context, session Session) error
	// GC performs a GC operation on the session store.
	GC(ctx context.Context) error
}

// Initer takes arbitrary number of arguments needed for initialization and
// returns an initialized session store.
type Initer func(ctx context.Context, args ...interface{}) (Store, error)

// manager is wrapper for wiring HTTP request and session stores.
type manager struct {
	store Store // The session store that is being managed.
}

// newManager returns a new manager with given session store.
func newManager(store Store) *manager {
	return &manager{
		store: store,
	}
}

// startGC starts a background goroutine to trigger GC of the session store in
// given time interval. Errors are printed using the `errFunc`. It returns a
// send-only channel for stopping the background goroutine.
func (m *manager) startGC(ctx context.Context, interval time.Duration, errFunc func(error)) chan<- struct{} {
	stop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(interval)
		for {
			err := m.store.GC(ctx)
			if err != nil {
				errFunc(err)
			}

			select {
			case <-stop:
				ticker.Stop()
				return
			case <-ticker.C:
			}
		}
	}()
	return stop
}

// randomChars returns a generated string in given number of random characters.
func randomChars(n int) (string, error) {
	const alphanum = "0123456789abcdefghijklmnopqrstuvwxyz"

	randomInt := func(max *big.Int) (int, error) {
		r, err := rand.Int(rand.Reader, max)
		if err != nil {
			return 0, err
		}

		return int(r.Int64()), nil
	}

	buffer := make([]byte, n)
	max := big.NewInt(int64(len(alphanum)))
	for i := 0; i < n; i++ {
		index, err := randomInt(max)
		if err != nil {
			return "", err
		}

		buffer[i] = alphanum[index]
	}

	return string(buffer), nil
}

// isValidSessionID returns true if given session ID looks like a valid ID.
func isValidSessionID(sid string, idLength int) bool {
	if len(sid) != idLength {
		return false
	}

	for i := range sid {
		switch {
		case '0' <= sid[i] && sid[i] <= '9':
		case 'a' <= sid[i] && sid[i] <= 'z':
		default:
			return false
		}
	}
	return true
}

// load loads the session from the session store with session ID provided in the
// named cookie. It returns `created=true` if a new session is created.
func (m *manager) load(r *http.Request, sid string, idLength int) (_ Session, created bool, err error) {
	if !isValidSessionID(sid, idLength) {
		sid, err = randomChars(idLength)
		if err != nil {
			return nil, false, errors.Wrap(err, "new ID")
		}
		created = true
	}

	sess, err := m.store.Read(r.Context(), sid)
	if err != nil {
		return nil, false, errors.Wrap(err, "read")
	}
	return sess, created, nil
}
