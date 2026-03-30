package pool

import (
	"strings"
	"sync"
	"testing"
)

func TestStringSlicePool_GetPut(t *testing.T) {
	t.Parallel()

	p := NewStringSlicePoolType(100)

	// Get a slice
	s := p.Get()
	if s == nil {
		t.Fatal("Get() returned nil")
	}
	if len(s) != 0 {
		t.Errorf("Get() returned slice with len %d, want 0", len(s))
	}
	if cap(s) != 100 {
		t.Errorf("Get() returned slice with cap %d, want 100", cap(s))
	}

	// Use it and put it back
	s = append(s, "test1", "test2")
	p.Put(s)

	// Get again — should be recycled
	s2 := p.Get()
	if len(s2) != 0 {
		t.Errorf("Get() after Put() returned slice with len %d, want 0", len(s2))
	}
}

func TestStringSlicePool_Concurrent(t *testing.T) {
	t.Parallel()

	p := NewStringSlicePoolType(50)
	const numGoroutines = 20
	const numOps = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				s := p.Get()
				s = append(s, "test")
				p.Put(s)
			}
		}()
	}

	wg.Wait()
}

func TestStringSlicePool_Global(t *testing.T) {
	t.Parallel()

	p := StringSlicePool()
	if p == nil {
		t.Fatal("StringSlicePool() returned nil")
	}

	s := p.Get()
	if s == nil {
		t.Fatal("Global pool Get() returned nil")
	}
	p.Put(s)
}

func TestStringBuilderPool_GetPut(t *testing.T) {
	t.Parallel()

	p := StringBuilderPool()

	builder := p.Get()
	if builder == nil {
		t.Fatal("Get() returned nil")
	}

	builder.WriteString("hello")
	if builder.String() != "hello" {
		t.Errorf("Builder content = %q, want %q", builder.String(), "hello")
	}

	p.Put(builder)

	// Get again — should be reset
	builder2 := p.Get()
	if builder2.Len() != 0 {
		t.Errorf("Get() after Put() returned builder with len %d, want 0", builder2.Len())
	}
	p.Put(builder2)
}

func TestStringBuilderPool_Concurrent(t *testing.T) {
	t.Parallel()

	p := StringBuilderPool()
	const numGoroutines = 20
	const numOps = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				b := p.Get()
				b.WriteString("test")
				_ = b.String()
				p.Put(b)
			}
		}()
	}

	wg.Wait()
}

func TestBytesPool_GetPut(t *testing.T) {
	t.Parallel()

	p := NewBytesPoolType(256)

	b := p.Get()
	if b == nil {
		t.Fatal("Get() returned nil")
	}
	if len(b) != 0 {
		t.Errorf("Get() returned bytes with len %d, want 0", len(b))
	}
	if cap(b) != 256 {
		t.Errorf("Get() returned bytes with cap %d, want 256", cap(b))
	}

	// Use and put back
	b = append(b, []byte("hello world")...)
	p.Put(b)

	// Get again
	b2 := p.Get()
	if len(b2) != 0 {
		t.Errorf("Get() after Put() returned bytes with len %d, want 0", len(b2))
	}
}

func TestBytesPool_Global(t *testing.T) {
	t.Parallel()

	p := BytesPool()
	if p == nil {
		t.Fatal("BytesPool() returned nil")
	}

	b := p.Get()
	if b == nil {
		t.Fatal("Global pool Get() returned nil")
	}
	p.Put(b)
}

func TestBytesPool_Concurrent(t *testing.T) {
	t.Parallel()

	p := BytesPool()
	const numGoroutines = 20
	const numOps = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				b := p.Get()
				b = append(b, []byte("test data")...)
				p.Put(b)
			}
		}()
	}

	wg.Wait()
}

func BenchmarkStringSlicePool(b *testing.B) {
	p := NewStringSlicePoolType(1000)
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		s := p.Get()
		s = append(s, "line1", "line2", "line3")
		p.Put(s)
	}
}

func BenchmarkStringBuilderPool(b *testing.B) {
	p := StringBuilderPool()
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		builder := p.Get()
		builder.WriteString(strings.Repeat("x", 100))
		_ = builder.String()
		p.Put(builder)
	}
}

func BenchmarkBytesPool(b *testing.B) {
	p := BytesPool()
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		buf := p.Get()
		buf = append(buf, []byte("sample data for benchmark")...)
		p.Put(buf)
	}
}
