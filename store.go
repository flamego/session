// Copyright 2021 Flamego. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package session

type Store interface {
	Read(sid string) (Session, error)
	Destroy(sid string) error
	GC() error
}

// todo: Initer takes a name and arbitrary number of parameters needed for initalization
// and returns an initalized logger.
type Initer func(args ...interface{}) (Store, error)
