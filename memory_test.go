// Copyright 2021 Flamego. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package session

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestMemoryStore(t *testing.T) {
	now := time.Now()
	store := newMemoryStore(
		MemoryConfig{
			nowFunc:  func() time.Time { return now },
			Lifetime: time.Second,
		},
	)

	sess1, err := store.Read("1")
	assert.Nil(t, err)

	now = now.Add(-time.Second)
	sess2, err := store.Read("2")
	assert.Nil(t, err)

	now = now.Add(-2 * time.Second)
	_, err = store.Read("3")
	assert.Nil(t, err)

	now = now.Add(2 * time.Second)
	err = store.GC() // sess3 should be recycled
	assert.Nil(t, err)

	wantHeap := []*memorySession{sess2.(*memorySession), sess1.(*memorySession)}
	assert.Equal(t, wantHeap, store.heap)

	wantIndex := map[string]*memorySession{
		"1": sess1.(*memorySession),
		"2": sess2.(*memorySession),
	}
	assert.Equal(t, wantIndex, store.index)
}
