// Copyright 2021 Flamego. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package session

import (
	"crypto/rand"
	"math/big"
	"time"

	"github.com/pkg/errors"

	"github.com/flamego/flamego"
)

// Store is a session store with capabilities of checking, reading, destroying
// and GC sessions.
type Store interface {
	// Exist returns true of the session with given ID exists.
	Exist(sid string) bool
	// Read returns the session with given ID. If a session with the ID does not
	// exist, a new session with the same ID is created and returned.
	Read(sid string) (Session, error)
	// Destroy deletes session with given ID from the session store completely.
	Destroy(sid string) error
	// GC performs a GC operation on the session store.
	GC() error
}

// Initer takes arbitrary number of arguments needed for initialization and
// returns an initialized session store.
type Initer func(args ...interface{}) (Store, error)

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
func (m *manager) startGC(interval time.Duration, errFunc func(error)) chan<- struct{} {
	stop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(interval)
		for {
			err := m.store.GC()
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
// named cookie. If a session with the ID does not exist, it creates a new
// session with the same ID. A boolean value is returned to indicate whether a
// new session is created.
func (m *manager) load(c flamego.Context, cookieName string, idLength int) (_ Session, created bool, err error) {
	sid := c.Cookie(cookieName)
	if isValidSessionID(sid, idLength) && m.store.Exist(sid) {
		sess, err := m.store.Read(sid)
		if err != nil {
			return nil, false, errors.Wrap(err, "read")
		}
		return sess, false, nil
	}

	sid, err = randomChars(idLength)
	if err != nil {
		return nil, false, errors.Wrap(err, "new ID")
	}

	sess, err := m.store.Read(sid)
	if err != nil {
		return nil, false, errors.Wrap(err, "read")
	}
	return sess, true, nil
}
