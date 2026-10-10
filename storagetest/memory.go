package storagetest

import (
	"bytes"
	"context"
	"io"
	"sync"
	"time"

	"github.com/angvp/tango/internal/storagekit"
	"github.com/angvp/tango/storage"
)

type memoryObject struct {
	info storage.Info
	data []byte
}

// Memory is an in-memory storage.Store for tests. It follows the same
// contract as every adapter (it passes the conformance suite) but keeps
// everything in the process and forgets it when the process ends.
type Memory struct {
	mu      sync.Mutex
	objects map[string]memoryObject
	nextKey func() string
}

// NewMemory returns an empty Memory.
func NewMemory() *Memory { return NewMemoryWithKeys(storage.NewKey) }

// NewMemoryWithKeys returns a Memory whose generated keys come from next. It
// exists so tests can force a collision; next must return valid keys.
func NewMemoryWithKeys(next func() string) *Memory {
	return &Memory{objects: map[string]memoryObject{}, nextKey: next}
}

// Put implements storage.Store.
func (m *Memory) Put(ctx context.Context, r io.Reader, opts storage.PutOptions) (storage.Object, error) {
	if err := ctx.Err(); err != nil {
		return storage.Object{}, err
	}
	inspected, err := storage.Inspect(r, opts)
	if err != nil {
		return storage.Object{}, err
	}
	data, err := io.ReadAll(inspected)
	if err != nil {
		return storage.Object{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for attempt := 0; attempt < storagekit.KeyAttempts; attempt++ {
		key := m.nextKey()
		if _, exists := m.objects[key]; exists {
			continue
		}
		obj := inspected.Object(key)
		m.objects[key] = memoryObject{
			data: data,
			info: storage.Info{
				Key: key, Size: obj.Size, SHA256: obj.SHA256, ContentType: obj.ContentType,
				ETag: storage.ETagFor(obj.SHA256), ModTime: time.Now().UTC(),
			},
		}
		return obj, nil
	}
	return storage.Object{}, storage.ErrExists
}

// Stat implements storage.Store.
func (m *Memory) Stat(ctx context.Context, key string) (storage.Info, error) {
	object, err := m.get(ctx, key)
	return object.info, err
}

// Open implements storage.Store.
func (m *Memory) Open(ctx context.Context, key string, offset, length int64) (io.ReadCloser, error) {
	object, err := m.get(ctx, key)
	if err != nil {
		return nil, err
	}
	size := int64(len(object.data))
	if offset < 0 || offset > size {
		return nil, storage.ErrInvalidRange
	}
	end := size
	if length >= 0 && offset+length < size {
		end = offset + length
	}
	return io.NopCloser(bytes.NewReader(object.data[offset:end])), nil
}

// Delete implements storage.Store.
func (m *Memory) Delete(ctx context.Context, key string) error {
	if err := storagekit.Check(ctx, key); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objects, key)
	return nil
}

func (m *Memory) get(ctx context.Context, key string) (memoryObject, error) {
	if err := storagekit.Check(ctx, key); err != nil {
		return memoryObject{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	object, ok := m.objects[key]
	if !ok {
		return memoryObject{}, storage.ErrNotFound
	}
	return object, nil
}
