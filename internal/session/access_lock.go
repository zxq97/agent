package session

import (
	"time"
)

type LockClient interface {
	SetNX(key string, ttl time.Duration) (bool, error)
	Del(key string) error
}

type AccessLock struct {
	client   LockClient
	prefix   string
	ttl      time.Duration
	failOpen bool
}

func NewAccessLock(client LockClient, prefix string, ttl time.Duration, failOpen bool) *AccessLock {
	if prefix == "" {
		prefix = "agent_lock"
	}
	if ttl <= 0 {
		ttl = time.Minute
	}
	return &AccessLock{client: client, prefix: prefix, ttl: ttl, failOpen: failOpen}
}

func (l *AccessLock) TryAcquire(userID, sessionID string) (func(), bool) {
	if l == nil || l.client == nil {
		return func() {}, true
	}
	key := l.prefix + ":" + userID + ":" + sessionID
	ok, err := l.client.SetNX(key, l.ttl)
	if err != nil {
		return func() {}, l.failOpen
	}
	if !ok {
		return func() {}, false
	}
	release := func() { _ = l.client.Del(key) }
	return release, true
}
