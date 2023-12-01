// Copyright 2021 Flamego. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package session

import (
	"bytes"
	"encoding/gob"
	"sync"
)

// Data is the data structure for storing session data.
type Data map[interface{}]interface{}

// Encoder is an encoder to encode session data to binary.
type Encoder func(Data) ([]byte, error)

// Decoder is a decoder to decode binary to session data.
type Decoder func([]byte) (Data, error)

var _ Session = (*BaseSession)(nil)

// BaseSession implements basic operations for the session data.
type BaseSession struct {
	sid     string       // The session ID
	lock    sync.RWMutex // The mutex to guard accesses to the data
	data    Data         // The map of the session data
	changed bool         // Whether the session has changed
	encoder Encoder      // The encoder to encode the session data to binary
}

// NewBaseSession returns a new BaseSession with given session ID.
func NewBaseSession(sid string, encoder Encoder) *BaseSession {
	return &BaseSession{
		sid:     sid,
		data:    make(Data),
		encoder: encoder,
	}
}

// NewBaseSessionWithData returns a new BaseSession with given session ID and
// initial data.
func NewBaseSessionWithData(sid string, encoder Encoder, data Data) *BaseSession {
	return &BaseSession{
		sid:     sid,
		data:    data,
		encoder: encoder,
	}
}

func (s *BaseSession) ID() string {
	return s.sid
}

func (s *BaseSession) Get(key interface{}) interface{} {
	s.lock.RLock()
	defer s.lock.RUnlock()
	return s.data[key]
}

func (s *BaseSession) Set(key, val interface{}) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.changed = true
	s.data[key] = val
}

func (s *BaseSession) SetFlash(val interface{}) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.changed = true
	s.data[flashKey] = val
}

func (s *BaseSession) Delete(key interface{}) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.changed = true
	delete(s.data, key)
}

func (s *BaseSession) Flush() {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.changed = true
	s.data = make(Data)
}

func (s *BaseSession) Encode() ([]byte, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()
	return s.encoder(s.data)
}

func (s *BaseSession) HasChanged() bool {
	s.lock.RLock()
	defer s.lock.RUnlock()
	return s.changed
}

// GobEncoder is a session data encoder using Gob.
func GobEncoder(data Data) ([]byte, error) {
	var buf bytes.Buffer
	err := gob.NewEncoder(&buf).Encode(data)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// GobDecoder is a session data decoder using Gob.
func GobDecoder(binary []byte) (Data, error) {
	buf := bytes.NewBuffer(binary)
	var data Data
	return data, gob.NewDecoder(buf).Decode(&data)
}

// Flash is anything that gets retrieved and deleted as soon as the next request
// happens.
type Flash interface{}

const flashKey = "flamego::session::flash"
