// Copyright 2021 Flamego. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package session

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/flamego/flamego"
)

func TestSessioner(t *testing.T) {
	f := flamego.NewWithLogger(&bytes.Buffer{})
	f.Use(Sessioner())
	f.Get("/", func(session Session, store Store) string {
		_ = store.GC()
		return session.ID()
	})

	resp := httptest.NewRecorder()
	req, err := http.NewRequest("GET", "/", nil)
	assert.Nil(t, err)

	f.ServeHTTP(resp, req)

	want := fmt.Sprintf("flamego_session=%s; Path=/; HttpOnly; SameSite=Lax", resp.Body.String())
	assert.Equal(t, want, resp.Header().Get("Set-Cookie"))
}
