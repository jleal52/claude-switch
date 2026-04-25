package store

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	tcmongo "github.com/testcontainers/testcontainers-go/modules/mongodb"
)

var (
	sharedMongoOnce sync.Once
	sharedMongoURI  string
	sharedMongoErr  error
)

// MustStartMongo boots a single Mongo testcontainer per test process and
// returns its connection URI. Skipped if Docker is not available.
func MustStartMongo(t *testing.T) string {
	t.Helper()
	sharedMongoOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		c, err := tcmongo.Run(ctx, "mongo:7")
		if err != nil {
			sharedMongoErr = err
			return
		}
		uri, err := c.ConnectionString(ctx)
		if err != nil {
			sharedMongoErr = err
			return
		}
		sharedMongoURI = uri
	})
	if sharedMongoErr != nil {
		t.Skipf("mongo testcontainer unavailable: %v", sharedMongoErr)
	}
	return sharedMongoURI
}

// NewTestStore returns a store backed by a fresh database name (so tests
// don't share state) on the shared mongo container.
func NewTestStore(t *testing.T, label string) *Store {
	t.Helper()
	uri := MustStartMongo(t)
	dbName := fmt.Sprintf("cs_test_%s_%d", label, time.Now().UnixNano())
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	s, err := New(ctx, uri, dbName)
	if err != nil {
		t.Fatalf("store new: %v", err)
	}
	t.Cleanup(func() {
		_ = s.db.Drop(context.Background())
		_ = s.Close(context.Background())
	})
	return s
}
