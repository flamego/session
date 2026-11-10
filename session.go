// Copyright 2021 Flamego. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package session

import (
	"context"
	"net/http"
	"reflect"
	"time"

	"github.com/pkg/errors"

	"github.com/flamego/flamego"
)

// Session is a session for the current request.
type Session interface {
	// ID returns the session ID.
	ID() string
	// RegenerateID regenerates the session ID.
	RegenerateID(w http.ResponseWriter, r *http.Request) error
	// Get returns the value of given key in the session. It returns nil if no such
	// key exists.
	Get(key interface{}) interface{}
	// Set sets the value of given key in the session.
	Set(key, val interface{})
	// SetFlash sets the flash to be the given value in the session.
	SetFlash(val interface{})
	// Delete deletes a key from the session.
	Delete(key interface{})
	// Flush wipes out all existing data in the session.
	Flush()
	// Encode encodes session data to binary.
	Encode() ([]byte, error)
	// HasChanged returns whether the session has changed.
	HasChanged() bool
}

// CookieOptions contains options for setting HTTP cookies.
type CookieOptions struct {
	// Name is the name of the cookie. Default is "flamego_session".
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
	// IDLength specifies the length of session IDs. Default is 16.
	IDLength int
	// GCInterval is the time interval for GC operations. Default is 5 minutes.
	GCInterval time.Duration
	// ErrorFunc is the function used to print errors when something went wrong on
	// the background. Default is to drop errors silently.
	ErrorFunc func(err error)
	// ReadIDFunc is the function to read session ID from the request. Default is
	// reading from cookie.
	ReadIDFunc func(r *http.Request) string
	// WriteIDFunc is the function to write session ID to the response. Default is
	// writing to cookie. The `created` argument indicates whether a new session was
	// created in the session store.
	WriteIDFunc func(w http.ResponseWriter, r *http.Request, sid string, created bool)

	nowFunc func() time.Time // For tests only
}

const deadlineKey = "flamego::session::deadline"

// SetDeadline sets an absolute deadline for the session, e.g. when the user is
// authenticated. Unlike the idle lifetime of the session store, the deadline is
// not extended by any later request. Once the deadline is reached, the
// session.Sessioner middleware destroys the session and starts a new one with a
// new ID, no matter whether the client still sends the old ID. A zero time
// removes the deadline.
//
// The deadline is stored with the session data, so it is enforced across
// restarts and multiple instances when using a persistent session store. It does
// not prevent the session from expiring earlier because of the idle lifetime of
// the session store or the expiry of the cookie.
func SetDeadline(s Session, t time.Time) {
	if t.IsZero() {
		s.Delete(deadlineKey)
		return
	}
	s.Set(deadlineKey, t.UnixNano())
}

// Deadline returns the absolute deadline of the session set by SetDeadline, and
// ok is false when no deadline is set.
func Deadline(s Session) (deadline time.Time, ok bool) {
	nsec, ok := s.Get(deadlineKey).(int64)
	if !ok {
		return time.Time{}, false
	}
	return time.Unix(0, nsec), true
}

const minimumSIDLength = 3

var ErrMinimumSIDLength = errors.Errorf("the SID does not have the minimum required length %d", minimumSIDLength)

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

		// NOTE: The file store requires at least 3 characters for the filename.
		if opts.IDLength < minimumSIDLength {
			opts.IDLength = 16
		}

		if opts.GCInterval.Seconds() < 1 {
			opts.GCInterval = 5 * time.Minute
		}

		if opts.nowFunc == nil {
			opts.nowFunc = time.Now
		}

		if opts.ErrorFunc == nil {
			opts.ErrorFunc = func(error) {}
		}

		if opts.ReadIDFunc == nil {
			opts.ReadIDFunc = func(r *http.Request) string {
				cookie, err := r.Cookie(opts.Cookie.Name)
				if err != nil {
					return ""
				}
				return cookie.Value
			}
		}
		if opts.WriteIDFunc == nil {
			opts.WriteIDFunc = func(w http.ResponseWriter, r *http.Request, sid string, created bool) {
				if !created {
					return
				}

				cookie := &http.Cookie{
					Name:     opts.Cookie.Name,
					Value:    sid,
					Path:     opts.Cookie.Path,
					Domain:   opts.Cookie.Domain,
					MaxAge:   opts.Cookie.MaxAge,
					Secure:   opts.Cookie.Secure,
					HttpOnly: opts.Cookie.HTTPOnly,
					SameSite: opts.Cookie.SameSite,
				}
				http.SetCookie(w, cookie)
				r.AddCookie(cookie)
			}
		}
		return opts
	}

	opt = parseOptions(opt)
	ctx := context.Background()

	store, err := opt.Initer(
		ctx,
		opt.Config,
		IDWriter(func(w http.ResponseWriter, r *http.Request, sid string) {
			opt.WriteIDFunc(w, r, sid, true)
		}),
	)
	if err != nil {
		panic("session: " + err.Error())
	}

	mgr := newManager(store)
	mgr.startGC(ctx, opt.GCInterval, opt.ErrorFunc)

	return flamego.ContextInvoker(func(c flamego.Context) {
		sid := opt.ReadIDFunc(c.Request().Request)
		sess, created, err := mgr.load(c.Request().Request, sid, opt.IDLength)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				c.ResponseWriter().WriteHeader(http.StatusUnprocessableEntity)
				return
			}
			panic("session: load: " + err.Error())
		}

		// Replace the session with a new one when its deadline has been reached.
		if deadline, ok := Deadline(sess); ok && !opt.nowFunc().Before(deadline) {
			err = store.Destroy(c.Request().Context(), sess.ID())
			if err == nil {
				sess, created, err = mgr.load(c.Request().Request, "", opt.IDLength)
			}
			if err != nil {
				if errors.Is(err, context.Canceled) {
					c.ResponseWriter().WriteHeader(http.StatusUnprocessableEntity)
					return
				}
				panic("session: replace expired: " + err.Error())
			}
		}
		opt.WriteIDFunc(c.ResponseWriter(), c.Request().Request, sess.ID(), created)

		flash := sess.Get(flashKey)
		if flash != nil {
			sess.Delete(flashKey)
		}

		c.Map(store, sess)
		c.MapTo(flash, (*Flash)(nil))
		c.Next()

		if sess.HasChanged() {
			err = store.Save(c.Request().Context(), sess)
		} else {
			err = store.Touch(c.Request().Context(), sess.ID())
		}
		if err != nil && !errors.Is(err, context.Canceled) {
			panic("session: save: " + err.Error())
		}
	})
}
