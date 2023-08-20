// Copyright 2021 Flamego. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package session

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsValidSessionID(t *testing.T) {
	for i := 0; i < 10; i++ {
		s, err := randomChars(16)
		require.Nil(t, err)
		assert.True(t, isValidSessionID(s, 16))
	}

	assert.False(t, isValidSessionID("123", 16))
	assert.False(t, isValidSessionID("3qKCBYmuAqG1RQix", 16))
	assert.False(t, isValidSessionID("../session/ad2c7", 16))
}

func TestManager_startGC(t *testing.T) {
	m := newManager(newMemoryStore(MemoryConfig{}))
	stop := m.startGC(
		context.Background(),
		time.Minute,
		func(error) { panic("unreachable") },
	)
	stop <- struct{}{}
}
