package ratelimit

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/alexander/bifrost/internal/store"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func newKey(t *testing.T, s *store.Store, perMin, perHour, perDay int) *store.APIKey {
	t.Helper()
	k := &store.APIKey{
		Name: "test", KeyHash: t.Name(), KeyPrefix: "bf_test",
		FromAddress: "x@example.com",
		ToAddresses: []string{"y@example.com"},
		RatePerMinute: perMin, RatePerHour: perHour, RatePerDay: perDay,
	}
	id, err := s.CreateAPIKey(context.Background(), k)
	if err != nil {
		t.Fatal(err)
	}
	k.ID = id
	return k
}

func TestPerMinuteAllow(t *testing.T) {
	s := newTestStore(t)
	k := newKey(t, s, 5, 0, 0)
	l := New(s)
	for i := 0; i < 5; i++ {
		d, err := l.Check(context.Background(), k)
		if err != nil {
			t.Fatal(err)
		}
		if !d.Allowed {
			t.Fatalf("call %d should be allowed", i+1)
		}
	}
	d, err := l.Check(context.Background(), k)
	if err != nil {
		t.Fatal(err)
	}
	if d.Allowed {
		t.Fatal("6th call should be denied")
	}
	if d.Bucket != "minute" {
		t.Fatalf("got bucket %q", d.Bucket)
	}
	if d.RetryAfter <= 0 || d.RetryAfter > time.Minute {
		t.Fatalf("unexpected retry-after %v", d.RetryAfter)
	}
}

func TestWindowReset(t *testing.T) {
	s := newTestStore(t)
	k := newKey(t, s, 2, 0, 0)
	now := time.Date(2025, 1, 1, 12, 0, 30, 0, time.UTC)
	l := New(s).withClock(func() time.Time { return now })

	for i := 0; i < 2; i++ {
		d, _ := l.Check(context.Background(), k)
		if !d.Allowed {
			t.Fatalf("call %d denied", i)
		}
	}
	d, _ := l.Check(context.Background(), k)
	if d.Allowed {
		t.Fatal("3rd should be denied within window")
	}

	// Advance past the minute boundary.
	now = now.Add(45 * time.Second)
	l = New(s).withClock(func() time.Time { return now })
	d, _ = l.Check(context.Background(), k)
	if !d.Allowed {
		t.Fatal("expected call to be allowed in new minute window")
	}
}

func TestUnlimited(t *testing.T) {
	s := newTestStore(t)
	k := newKey(t, s, 0, 0, 0)
	l := New(s)
	for i := 0; i < 50; i++ {
		d, err := l.Check(context.Background(), k)
		if err != nil {
			t.Fatal(err)
		}
		if !d.Allowed {
			t.Fatal("unlimited key was denied")
		}
	}
}

func TestHourBlocksBeforeMinute(t *testing.T) {
	// per-hour limit reaches first.
	s := newTestStore(t)
	k := newKey(t, s, 0, 3, 0)
	l := New(s)
	for i := 0; i < 3; i++ {
		l.Check(context.Background(), k)
	}
	d, _ := l.Check(context.Background(), k)
	if d.Allowed {
		t.Fatal("expected hourly cap to deny")
	}
	if d.Bucket != "hour" {
		t.Fatalf("got bucket %q", d.Bucket)
	}
}
