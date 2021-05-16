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

// Session is a session for the current request.
type Session interface {
	// ID returns the session ID.
	ID() string
	// Get returns the value of given key in the session. It returns nil if no such
	// key exists.
	Get(key interface{}) interface{}
	// Set sets the value of given key in the session.
	Set(key, val interface{})
	// Delete deletes a key from the session.
	Delete(key interface{})
	// Flush wipes out all existing data in the session.
	Flush()
	// Save persists current session to its store.
	Save() error
}

// CookieOptions contains options for setting HTTP cookies.
type CookieOptions struct {
	// Name is the name of the cookie.
	Name string
	// Path is the Path attribute of the cookie. Default is "/".
	Path string
	// Domain is the Domain attribute of the cookie. Default is not set.
	Domain string
	// MaxAge is the MaxAge attribute of the cookie. Default is not set.
	MaxAge int
	// Secure specifies whether to set Secure for the cookie.
	Secure bool
	// HTTPOnly specifies whether to set HTTPOnly for the cookie.
	HTTPOnly bool
	// SameSite is the SameSite attribute of the cookie. Default is
	// http.SameSiteLaxMode.
	SameSite http.SameSite
}

// Options contains options for the session.Sessioner middleware.
type Options struct {
	// Initer is the initialization function of the session store. Default is
	// session.MemoryIniter.
	Initer Initer
	// Config is the configuration object to be passed to the Initer for the session
	// store.
	Config interface{}
	// Cookie is a set of options for setting HTTP cookies.
	Cookie CookieOptions
	// IDLength specifies the length of session IDs.
	IDLength int
	// GCInterval is the time interval for GC operations.
	GCInterval time.Duration
	// ErrorFunc is the function used to print errors when something went wrong on
	// the background.
	ErrorFunc func(err error)
}

// Sessioner returns a middleware handler that injects session.Session and
// session.Store into the request context, which are used for manipulating
// session data.
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
