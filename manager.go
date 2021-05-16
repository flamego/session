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

type manager struct {
	store Store
}

func newManager(store Store) *manager {
	return &manager{
		store: store,
	}
}

func (m *manager) startGC(interval time.Duration, errFunc func(error)) <-chan struct{} {
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
				return
			case <-ticker.C:
			}
		}
	}()
	return stop
}

// randomChars returns a generated string in given number of random characters.
func randomChars(n int) (string, error) {
	const alphanum = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

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

// todo: isValidSessionID tests whether a provided session ID is a valid session ID.
func (m *manager) isValidSessionID(sid string, idLength int) bool {
	if len(sid) != idLength {
		return false
	}

	for i := range sid {
		switch {
		case '0' <= sid[i] && sid[i] <= '9':
		case 'a' <= sid[i] && sid[i] <= 'f':
		default:
			return false
		}
	}
	return true
}

func (m *manager) start(c flamego.Context, cookieName string, idLength int) (_ Session, created bool, err error) {
	sid := c.Cookie(cookieName)
	if m.isValidSessionID(sid, idLength) && m.store.Exist(sid) {
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
