package session

import (
	"errors"
	"testing"
	"time"
)

func TestAccessLockSerializesSameSession(t *testing.T) {
	client := newFakeLockClient()
	lock := NewAccessLock(client, "agent_lock", time.Minute, false)

	release, ok := lock.TryAcquire("u1", "s1")
	if !ok {
		t.Fatal("first acquire failed")
	}
	if _, ok := lock.TryAcquire("u1", "s1"); ok {
		t.Fatal("second acquire succeeded, want blocked")
	}
	release()
	if _, ok := lock.TryAcquire("u1", "s1"); !ok {
		t.Fatal("acquire after release failed")
	}
}

func TestAccessLockFailOpen(t *testing.T) {
	client := newFakeLockClient()
	client.err = errors.New("redis down")
	lock := NewAccessLock(client, "agent_lock", time.Minute, true)

	_, ok := lock.TryAcquire("u1", "s1")

	if !ok {
		t.Fatal("fail-open lock should allow on redis error")
	}
}

type fakeLockClient struct {
	held map[string]bool
	err  error
}

func newFakeLockClient() *fakeLockClient {
	return &fakeLockClient{held: map[string]bool{}}
}

func (f *fakeLockClient) SetNX(key string, ttl time.Duration) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	if f.held[key] {
		return false, nil
	}
	f.held[key] = true
	return true, nil
}

func (f *fakeLockClient) Del(key string) error {
	delete(f.held, key)
	return nil
}
