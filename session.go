// Copyright 2021 Flamego. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package session

import (
	"net/http"
	"reflect"
	"time"

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

type CookieOptions struct {
	Name     string
	Path     string
	Domain   string
	MaxAge   int
	Secure   bool
	HTTPOnly bool
	SameSite http.SameSite
}

type Options struct {
	Initer     Initer
	Config     interface{}
	Cookie     CookieOptions
	IDLength   int
	GCInterval time.Duration
	ErrorFunc  func(err error)
}

func Sessioner(opts ...Options) flamego.Handler {
	var opt Options
	if len(opts) > 0 {
		opt = opts[0]
	}

	parseOptions := func(opts Options) Options {
		if opts.Initer == nil {
			opts.Initer = MemoryIniter()
		}

		if reflect.DeepEqual(opts.Cookie, CookieOptions{}) {
			opts.Cookie = CookieOptions{
				HTTPOnly: true,
			}
		}
		if opts.Cookie.Name == "" {
			opts.Cookie.Name = "flamego_session"
		}
		if opts.Cookie.SameSite < http.SameSiteDefaultMode || opts.Cookie.SameSite > http.SameSiteNoneMode {
			opts.Cookie.SameSite = http.SameSiteLaxMode
		}
		if opts.Cookie.Path == "" {
			opts.Cookie.Path = "/"
		}

		if opts.IDLength <= 0 {
			opts.IDLength = 16
		}

		if opts.GCInterval.Seconds() < 1 {
			opts.GCInterval = 5 * time.Minute
		}
		return opts
	}

	opt = parseOptions(opt)

	store, err := opt.Initer(opt.Config)
	if err != nil {
		panic("session: " + err.Error())
	}

	mgr := newManager(store)
	mgr.startGC(opt.GCInterval, opt.ErrorFunc)

	return flamego.ContextInvoker(func(c flamego.Context) {
		sess, created, err := mgr.load(c, opt.Cookie.Name, opt.IDLength)
		if err != nil {
			panic("session: load: " + err.Error())
		}

		if created {
			cookie := &http.Cookie{
				Name:     opt.Cookie.Name,
				Value:    sess.ID(),
				Path:     opt.Cookie.Path,
				Domain:   opt.Cookie.Domain,
				MaxAge:   opt.Cookie.MaxAge,
				Secure:   opt.Cookie.Secure,
				HttpOnly: opt.Cookie.HTTPOnly,
				SameSite: opt.Cookie.SameSite,
			}
			http.SetCookie(c.ResponseWriter(), cookie)
			c.Request().AddCookie(cookie)
		}

		c.Map(store).Map(sess)
		c.Next()

		err = sess.Save()
		if err != nil {
			panic("session: save: " + err.Error())
		}
	})
}
