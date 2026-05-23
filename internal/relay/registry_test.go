package relay

import (
	"net"
	"sync"
	"testing"
)

// fakeTunnel is a no-op Tunnel for registry tests.
type fakeTunnel struct{ closed bool }

func (f *fakeTunnel) OpenStream() (net.Conn, error) { return nil, nil }
func (f *fakeTunnel) Close() error                  { f.closed = true; return nil }

func TestRegistryTryAddGetRemove(t *testing.T) {
	r := NewRegistry()
	tun := &fakeTunnel{}

	if !r.TryAdd("happy-fox-0001", tun) {
		t.Fatal("first TryAdd should succeed")
	}
	if r.TryAdd("happy-fox-0001", &fakeTunnel{}) {
		t.Fatal("duplicate TryAdd should fail")
	}

	got, ok := r.Get("happy-fox-0001")
	if !ok || got != tun {
		t.Fatalf("Get = %v, %v; want the original tunnel", got, ok)
	}
	if r.Count() != 1 {
		t.Fatalf("Count = %d, want 1", r.Count())
	}

	r.Remove("happy-fox-0001")
	if _, ok := r.Get("happy-fox-0001"); ok {
		t.Fatal("Get should fail after Remove")
	}
	if r.Count() != 0 {
		t.Fatalf("Count = %d, want 0", r.Count())
	}
}

func TestRegistryConcurrentAdd(t *testing.T) {
	r := NewRegistry()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			r.TryAdd(GenerateName(n), &fakeTunnel{})
		}(i)
	}
	wg.Wait()
	if r.Count() != 50 {
		t.Fatalf("Count = %d, want 50", r.Count())
	}
}

// GenerateName is a deterministic helper for the concurrency test.
func GenerateName(n int) string { return "sub-" + string(rune('a'+n%26)) + "-" + itoa(n) }

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
