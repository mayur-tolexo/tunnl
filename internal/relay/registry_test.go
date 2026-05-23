package relay

import (
	"net"
	"strconv"
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
			r.TryAdd(generateName(n), &fakeTunnel{})
		}(i)
	}
	wg.Wait()
	if r.Count() != 50 {
		t.Fatalf("Count = %d, want 50", r.Count())
	}
}

func TestTryReserve(t *testing.T) {
	t.Run("duplicate rejected", func(t *testing.T) {
		r := NewRegistry()
		if !r.TryReserve("sub-a", &fakeTunnel{}, 0) {
			t.Fatal("first TryReserve should succeed")
		}
		if r.TryReserve("sub-a", &fakeTunnel{}, 0) {
			t.Fatal("duplicate TryReserve should fail")
		}
	})

	t.Run("cap enforced at limit", func(t *testing.T) {
		r := NewRegistry()
		if !r.TryReserve("sub-1", &fakeTunnel{}, 1) {
			t.Fatal("first entry under cap should succeed")
		}
		if r.TryReserve("sub-2", &fakeTunnel{}, 1) {
			t.Fatal("second entry at cap should fail")
		}
	})

	t.Run("cap of 0 is unlimited", func(t *testing.T) {
		r := NewRegistry()
		for i := 0; i < 100; i++ {
			if !r.TryReserve("sub-"+strconv.Itoa(i), &fakeTunnel{}, 0) {
				t.Fatalf("TryReserve(%d) with max=0 should always succeed", i)
			}
		}
	})
}

func TestReplace(t *testing.T) {
	t.Run("succeeds when old matches", func(t *testing.T) {
		r := NewRegistry()
		old := &fakeTunnel{}
		new := &fakeTunnel{}
		r.TryAdd("sub-x", old)
		if !r.Replace("sub-x", old, new) {
			t.Fatal("Replace should succeed when old matches")
		}
		got, _ := r.Get("sub-x")
		if got != new {
			t.Fatal("Get should return new tunnel after Replace")
		}
	})

	t.Run("fails when old does not match", func(t *testing.T) {
		r := NewRegistry()
		actual := &fakeTunnel{}
		other := &fakeTunnel{}
		new := &fakeTunnel{}
		r.TryAdd("sub-y", actual)
		if r.Replace("sub-y", other, new) {
			t.Fatal("Replace should fail when old does not match current")
		}
		got, _ := r.Get("sub-y")
		if got != actual {
			t.Fatal("Get should still return original tunnel after failed Replace")
		}
	})
}

// generateName is a deterministic helper for the concurrency test.
func generateName(n int) string { return "sub-" + string(rune('a'+n%26)) + "-" + strconv.Itoa(n) }
