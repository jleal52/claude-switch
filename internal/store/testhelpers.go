package store

import (
	"context"
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
