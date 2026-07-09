package session

import (
	"strings"
	"testing"
	"time"

	"github.com/zxq97/rental-agent/internal/config"
	"github.com/zxq97/rental-agent/internal/llm"
	"github.com/zxq97/rental-agent/internal/orchestration"
)

func TestNewStoreFallsBackToMemoryWhenRedisAddrEmpty(t *testing.T) {
	store := NewStore(config.SessionConf{TTLHours: 3})

	if _, ok := store.(*MemoryStore); !ok {
		t.Fatalf("NewStore without redis addr = %T, want *MemoryStore", store)
	}
}

func TestRedisStoreRoundTripListAndDelete(t *testing.T) {
	client := newFakeRedisClient()
	store := newRedisStoreWithClient(client, "agent:test", time.Hour)
	st := orchestration.New("s1", "u1")
	st.AppendMessage(&llm.Message{Role: llm.RoleUser, Content: "明天北京 SUV"}, nil)

	store.Put("u1", "s1", st)

	got := store.Get("u1", "s1")
	if got == nil {
		t.Fatal("Get returned nil")
	}
	if got.SessionID != "s1" || got.UserID != "u1" {
		t.Fatalf("state = %#v", got)
	}
	list := store.List("u1")
	if len(list) != 1 || list[0].SessionID != "s1" || list[0].Preview != "明天北京 SUV" {
		t.Fatalf("List = %#v", list)
	}
	if client.ttl["agent:test:u1:s1"] != time.Hour {
		t.Fatalf("ttl = %s, want 1h", client.ttl["agent:test:u1:s1"])
	}

	store.Delete("u1", "s1")
	if got := store.Get("u1", "s1"); got != nil {
		t.Fatalf("Get after delete = %#v, want nil", got)
	}
}

func TestRedisStoreFallsBackToMemoryOnRedisErrors(t *testing.T) {
	client := newFakeRedisClient()
	client.err = true
	var logs strings.Builder
	store := newRedisStoreWithClient(client, "agent:test", time.Hour, &logs)
	st := orchestration.New("s-fallback", "u1")
	st.Rental.PickupName = "首都机场T3"

	store.Put("u1", "s-fallback", st)
	got := store.Get("u1", "s-fallback")

	if got == nil {
		t.Fatal("Get returned nil from fallback")
	}
	if got.Rental.PickupName != "首都机场T3" {
		t.Fatalf("fallback state lost rental: %+v", got.Rental)
	}
	list := store.List("u1")
	if len(list) != 1 || list[0].SessionID != "s-fallback" {
		t.Fatalf("fallback list = %+v", list)
	}
	for _, want := range []string{
		"[session] store=redis op=put status=redis_error",
		"[session] store=redis op=get status=redis_error",
		"[session] store=redis op=list status=redis_error",
	} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("logs missing %q:\n%s", want, logs.String())
		}
	}
}

type fakeRedisClient struct {
	data map[string]string
	ttl  map[string]time.Duration
	err  bool
}

func newFakeRedisClient() *fakeRedisClient {
	return &fakeRedisClient{data: map[string]string{}, ttl: map[string]time.Duration{}}
}

func (f *fakeRedisClient) Get(key string) (string, bool, error) {
	if f.err {
		return "", false, errFakeRedis
	}
	v, ok := f.data[key]
	return v, ok, nil
}

func (f *fakeRedisClient) SetEX(key, value string, ttl time.Duration) error {
	if f.err {
		return errFakeRedis
	}
	f.data[key] = value
	f.ttl[key] = ttl
	return nil
}

func (f *fakeRedisClient) Del(key string) error {
	if f.err {
		return errFakeRedis
	}
	delete(f.data, key)
	delete(f.ttl, key)
	return nil
}

func (f *fakeRedisClient) Keys(pattern string) ([]string, error) {
	if f.err {
		return nil, errFakeRedis
	}
	var out []string
	for k := range f.data {
		if matchRedisGlob(pattern, k) {
			out = append(out, k)
		}
	}
	return out, nil
}

var errFakeRedis = fakeRedisError{}

type fakeRedisError struct{}

func (fakeRedisError) Error() string { return "fake redis error" }
