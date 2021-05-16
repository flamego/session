// Copyright 2021 Flamego. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package session

import (
	"github.com/flamego/flamego"
)

type Session interface {
	ID() string
	Get(key interface{}) interface{}
	Set(key, val interface{})
	Delete(key interface{})
	Flush()
	Save() error
}

type session struct {
}

type Options struct {
}

func Sessioner(opts ...Options) flamego.Handler {
	return func() {}
}
