// Package pool provides object pools for frequently allocated types.
// Using sync.Pool reduces GC pressure and improves performance in hot paths
// with per-P affinity for minimal contention.
package pool

import (
	"strings"
	"sync"
)

// stringSlicePool is a pool of string slices used for log buffers.
var stringSlicePool = &StringSlicePoolType{
	capacity: 1000,
	pool: sync.Pool{
		New: func() any {
			return make([]string, 0, 1000)
		},
	},
}

// StringSlicePoolType manages a pool of string slices using sync.Pool.
type StringSlicePoolType struct {
	capacity int
	pool     sync.Pool
}

// NewStringSlicePoolType creates a new StringSlicePoolType with the specified capacity.
func NewStringSlicePoolType(capacity int) *StringSlicePoolType {
	return &StringSlicePoolType{
		capacity: capacity,
		pool: sync.Pool{
			New: func() any {
				return make([]string, 0, capacity)
			},
		},
	}
}

// Get returns a string slice from the pool or creates a new one.
func (p *StringSlicePoolType) Get() []string {
	s := p.pool.Get().([]string)
	return s[:0]
}

// Put returns a string slice to the pool.
func (p *StringSlicePoolType) Put(slice []string) {
	// Clear references to allow GC of strings
	clear(slice)
	p.pool.Put(slice[:0]) //nolint:staticcheck
}

// StringSlicePool returns the global string slice pool.
func StringSlicePool() *StringSlicePoolType {
	return stringSlicePool
}

// stringBuilderPool is a pool of strings.Builder for efficient string concatenation.
var stringBuilderPool = &StringBuilderPoolType{
	pool: sync.Pool{
		New: func() any {
			return &strings.Builder{}
		},
	},
}

// StringBuilderPoolType manages a pool of strings.Builder instances using sync.Pool.
type StringBuilderPoolType struct {
	pool sync.Pool
}

// Get returns a strings.Builder from the pool or creates a new one.
func (p *StringBuilderPoolType) Get() *strings.Builder {
	builder := p.pool.Get().(*strings.Builder)
	builder.Reset()
	return builder
}

// Put returns a strings.Builder to the pool.
func (p *StringBuilderPoolType) Put(builder *strings.Builder) {
	p.pool.Put(builder)
}

// StringBuilderPool returns the global string builder pool.
func StringBuilderPool() *StringBuilderPoolType {
	return stringBuilderPool
}

// bytesPool is a pool of byte slices for I/O operations.
var bytesPool = &BytesPoolType{
	capacity: 4096,
	pool: sync.Pool{
		New: func() any {
			return make([]byte, 0, 4096)
		},
	},
}

// BytesPoolType manages a pool of byte slices using sync.Pool.
type BytesPoolType struct {
	capacity int
	pool     sync.Pool
}

// NewBytesPoolType creates a new BytesPoolType with the specified capacity.
func NewBytesPoolType(capacity int) *BytesPoolType {
	return &BytesPoolType{
		capacity: capacity,
		pool: sync.Pool{
			New: func() any {
				return make([]byte, 0, capacity)
			},
		},
	}
}

// Get returns a byte slice from the pool or creates a new one.
func (p *BytesPoolType) Get() []byte {
	b := p.pool.Get().([]byte)
	return b[:0]
}

// Put returns a byte slice to the pool.
func (p *BytesPoolType) Put(bytes []byte) {
	p.pool.Put(bytes[:0]) //nolint:staticcheck
}

// BytesPool returns the global byte slice pool.
func BytesPool() *BytesPoolType {
	return bytesPool
}
