# Server (subsystem 2) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the central Go HTTP+WebSocket server (`claude-switch-server`) that authenticates users via GitHub/Google OAuth, persists a multi-tenant catalog in MongoDB, completes the device-code pairing flow defined by the wrapper, and relays PTY traffic between browsers and wrappers.

**Architecture:** Single Go binary inside the existing `claude-switch` monorepo. `net/http` stdlib router; `coder/websocket` for both wrapper and browser sockets; `mongo-driver/v2` for persistence; `golang.org/x/oauth2` for GitHub+Google. SPA bundle embedded via `go:embed` by default; build tag `noweb` produces a headless API binary. Deployed via Docker Compose joining the user's existing Traefik+Mongo container networks. The in-memory hub owns session↔wrapper↔browsers routing — single instance only in MVP.

**Tech Stack:** Go 1.24, `net/http`, `coder/websocket`, `go.mongodb.org/mongo-driver/v2`, `golang.org/x/oauth2`, `golang.org/x/crypto/bcrypt`, `oklog/ulid/v2` (existing). Tests: `stretchr/testify`, `testcontainers-go` (mongo module), `httptest`.

**Spec:** `docs/superpowers/specs/2026-04-25-server-design.md` — read it first; this plan implements what that doc specifies, nothing more.

**Subsystem 1 carryover:** the wrapper's wire protocol and the auth package (device-code, refresh, credentials) are already in `internal/proto/` and `internal/auth/`. This plan extends them rather than replacing.

---

## Task 1: Bootstrap server binary + Dockerfile

**Goal:** Empty `claude-switch-server` builds, prints version, exits. Dockerfile builds the binary in a multi-stage image. `go test ./...` still green.

**Files:**
- Create: `cmd/claude-switch-server/main.go`
- Create: `Dockerfile.server`
- Create: `web/.gitkeep`
- Modify: `Makefile`

- [ ] **Step 1: Create the placeholder web directory**

```bash
mkdir -p web
echo "# Frontend bundle for subsystem 3 lives here." > web/.gitkeep
```

- [ ] **Step 2: Create main.go scaffold**

`cmd/claude-switch-server/main.go`:

```go
// Command claude-switch-server is the multi-tenant relay between browsers
// and wrappers. See docs/superpowers/specs/2026-04-25-server-design.md.
package main

import (
	"flag"
	"fmt"
	"os"
)

const serverVersion = "0.1.0-dev"

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(serverVersion)
		return
	}
	fmt.Fprintln(os.Stderr, "claude-switch-server: not implemented yet (Task 1 bootstrap)")
	os.Exit(0)
}
```

- [ ] **Step 3: Add Dockerfile.server**

`Dockerfile.server`:

```dockerfile
# syntax=docker/dockerfile:1

FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
        -o /out/claude-switch-server ./cmd/claude-switch-server

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/claude-switch-server /usr/local/bin/claude-switch-server
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/claude-switch-server"]
```

- [ ] **Step 4: Extend Makefile**

Add to `Makefile`:

```makefile
build-server:
	go build -o bin/claude-switch-server ./cmd/claude-switch-server

docker-server:
	docker build -f Dockerfile.server -t claude-switch-server:dev .
```

Append `build-server docker-server` to the `.PHONY` line at the top.

- [ ] **Step 5: Verify build + test**

```bash
go build ./...
go test ./...
go run ./cmd/claude-switch-server -version
```

Expected: build/test green; `-version` prints `0.1.0-dev`.

- [ ] **Step 6: Commit**

```bash
git add cmd/claude-switch-server Dockerfile.server Makefile web/.gitkeep
git commit -m "feat(server): bootstrap claude-switch-server binary + Dockerfile"
```

---

## Task 2: Mongo store base — connect + index init

**Goal:** `store.New(ctx, uri, dbName)` returns a `*Store` connected to Mongo, ensures all required indexes exist, and exposes typed accessors used by later tasks. Tested against a real Mongo via `testcontainers-go`.

**Files:**
- Create: `internal/store/store.go`
- Create: `internal/store/store_test.go`
- Create: `internal/store/testhelpers.go`

- [ ] **Step 1: Add dependencies**

```bash
go get go.mongodb.org/mongo-driver/v2/mongo@latest
go get github.com/testcontainers/testcontainers-go@latest
go get github.com/testcontainers/testcontainers-go/modules/mongodb@latest
```

- [ ] **Step 2: Write failing test**

`internal/store/store_test.go`:

```go
package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNewConnectsAndPings(t *testing.T) {
	uri := MustStartMongo(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	s, err := New(ctx, uri, "claude_switch_test")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	require.NoError(t, s.Ping(ctx))
}

func TestNewCreatesAllRequiredIndexes(t *testing.T) {
	uri := MustStartMongo(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	s, err := New(ctx, uri, "claude_switch_idx")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	cases := []struct {
		coll   string
		indexes []string // index name fragments to expect
	}{
		{"users", []string{"oauth_provider_1_oauth_subject_1", "email_1"}},
		{"wrappers", []string{"user_id_1_paired_at_-1", "refresh_token_id_1"}},
		{"wrapper_access_tokens", []string{"token_hash_1", "expires_at_1"}},
		{"pairing_codes", []string{"code_1", "expires_at_1"}},
		{"sessions", []string{"user_id_1_created_at_-1", "wrapper_id_1_status_1"}},
		{"session_messages", []string{"session_id_1_ts_1", "user_id_1_ts_-1", "ts_1"}},
		{"auth_sessions", []string{"user_id_1", "expires_at_1"}},
	}
	for _, c := range cases {
		got, err := s.IndexNames(ctx, c.coll)
		require.NoError(t, err, "collection %s", c.coll)
		for _, want := range c.indexes {
			require.Contains(t, got, want, "collection %s missing index %s", c.coll, want)
		}
	}
}
```

- [ ] **Step 3: Write the test helper**

`internal/store/testhelpers.go`:

```go
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
```

- [ ] **Step 4: Run test to verify it fails**

```bash
go test ./internal/store/...
```

Expected: compile failure — `undefined: New`, `undefined: Store`.

- [ ] **Step 5: Implement store.go**

`internal/store/store.go`:

```go
// Package store wraps the MongoDB persistence layer. Each repository file
// (users.go, wrappers.go, ...) hangs methods off *Store; this file owns the
// connection lifecycle and index creation.
package store

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type Store struct {
	client *mongo.Client
	db     *mongo.Database
}

// New connects to Mongo, pings, and ensures all collections + indexes exist.
func New(ctx context.Context, uri, dbName string) (*Store, error) {
	cli, err := mongo.Connect(options.Client().ApplyURI(uri).SetServerSelectionTimeout(8 * time.Second))
	if err != nil {
		return nil, fmt.Errorf("mongo connect: %w", err)
	}
	s := &Store{client: cli, db: cli.Database(dbName)}
	if err := s.Ping(ctx); err != nil {
		_ = s.Close(context.Background())
		return nil, err
	}
	if err := s.ensureIndexes(ctx); err != nil {
		_ = s.Close(context.Background())
		return nil, err
	}
	return s, nil
}

func (s *Store) Ping(ctx context.Context) error {
	return s.client.Ping(ctx, nil)
}

func (s *Store) Close(ctx context.Context) error {
	return s.client.Disconnect(ctx)
}

// IndexNames returns the index names of a collection (handy for tests).
func (s *Store) IndexNames(ctx context.Context, coll string) ([]string, error) {
	cur, err := s.db.Collection(coll).Indexes().List(ctx)
	if err != nil {
		return nil, err
	}
	var out []string
	for cur.Next(ctx) {
		var spec struct {
			Name string `bson:"name"`
		}
		if err := cur.Decode(&spec); err != nil {
			return nil, err
		}
		out = append(out, spec.Name)
	}
	return out, cur.Err()
}

func (s *Store) ensureIndexes(ctx context.Context) error {
	// Each entry is (collection, model). Names follow Mongo's auto-naming
	// convention (field_direction joined by underscore) so tests can assert.
	type idxSpec struct {
		coll  string
		model mongo.IndexModel
	}
	specs := []idxSpec{
		{"users", mongo.IndexModel{
			Keys:    bson.D{{Key: "oauth_provider", Value: 1}, {Key: "oauth_subject", Value: 1}},
			Options: options.Index().SetUnique(true),
		}},
		{"users", mongo.IndexModel{Keys: bson.D{{Key: "email", Value: 1}}}},

		{"wrappers", mongo.IndexModel{
			Keys: bson.D{{Key: "user_id", Value: 1}, {Key: "paired_at", Value: -1}},
		}},
		{"wrappers", mongo.IndexModel{
			Keys:    bson.D{{Key: "refresh_token_id", Value: 1}},
			Options: options.Index().SetUnique(true),
		}},

		{"wrapper_access_tokens", mongo.IndexModel{
			Keys:    bson.D{{Key: "token_hash", Value: 1}},
			Options: options.Index().SetUnique(true),
		}},
		{"wrapper_access_tokens", mongo.IndexModel{
			Keys:    bson.D{{Key: "expires_at", Value: 1}},
			Options: options.Index().SetExpireAfterSeconds(0),
		}},

		{"pairing_codes", mongo.IndexModel{
			Keys:    bson.D{{Key: "code", Value: 1}},
			Options: options.Index().SetUnique(true),
		}},
		{"pairing_codes", mongo.IndexModel{
			Keys:    bson.D{{Key: "expires_at", Value: 1}},
			Options: options.Index().SetExpireAfterSeconds(0),
		}},

		{"sessions", mongo.IndexModel{
			Keys: bson.D{{Key: "user_id", Value: 1}, {Key: "created_at", Value: -1}},
		}},
		{"sessions", mongo.IndexModel{
			Keys: bson.D{{Key: "wrapper_id", Value: 1}, {Key: "status", Value: 1}},
		}},

		{"session_messages", mongo.IndexModel{
			Keys: bson.D{{Key: "session_id", Value: 1}, {Key: "ts", Value: 1}},
		}},
		{"session_messages", mongo.IndexModel{
			Keys: bson.D{{Key: "user_id", Value: 1}, {Key: "ts", Value: -1}},
		}},
		{"session_messages", mongo.IndexModel{
			Keys:    bson.D{{Key: "ts", Value: 1}},
			Options: options.Index().SetExpireAfterSeconds(90 * 24 * 3600),
		}},

		{"auth_sessions", mongo.IndexModel{Keys: bson.D{{Key: "user_id", Value: 1}}}},
		{"auth_sessions", mongo.IndexModel{
			Keys:    bson.D{{Key: "expires_at", Value: 1}},
			Options: options.Index().SetExpireAfterSeconds(0),
		}},
	}
	for _, sp := range specs {
		if _, err := s.db.Collection(sp.coll).Indexes().CreateOne(ctx, sp.model); err != nil {
			return fmt.Errorf("index on %s: %w", sp.coll, err)
		}
	}
	return nil
}
```

- [ ] **Step 6: Run tests**

```bash
go test ./internal/store/... -timeout 90s
```

Expected: both tests PASS (first run downloads the mongo:7 image once).

- [ ] **Step 7: Commit**

```bash
git add internal/store go.mod go.sum
git commit -m "feat(store): mongo connection + ensureIndexes for all collections"
```

---

## Task 3: Users repo

**Goal:** `Users` repository — Upsert by (provider, subject), GetByID, MarkLogin, SetKeepTranscripts.

**Files:**
- Create: `internal/store/users.go`
- Create: `internal/store/users_test.go`

- [ ] **Step 1: Write failing test**

`internal/store/users_test.go`:

```go
package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestUsersUpsertCreatesThenReturnsSame(t *testing.T) {
	s := newTestStore(t, "users_upsert")
	ctx := context.Background()

	u1, err := s.Users().UpsertOAuth(ctx, OAuthProfile{
		Provider: "github", Subject: "1234", Email: "a@example.com",
		Name: "Ada", AvatarURL: "https://...",
	})
	require.NoError(t, err)
	require.NotEmpty(t, u1.ID)
	require.False(t, u1.CreatedAt.IsZero())

	// Same (provider, subject) returns the same row, with updated profile fields.
	u2, err := s.Users().UpsertOAuth(ctx, OAuthProfile{
		Provider: "github", Subject: "1234", Email: "a@example.com",
		Name: "Ada Lovelace", AvatarURL: "https://...new",
	})
	require.NoError(t, err)
	require.Equal(t, u1.ID, u2.ID)
	require.Equal(t, "Ada Lovelace", u2.Name)
	require.Equal(t, "https://...new", u2.AvatarURL)
}

func TestUsersGetByIDNotFound(t *testing.T) {
	s := newTestStore(t, "users_getmissing")
	ctx := context.Background()

	_, err := s.Users().GetByID(ctx, "000000000000000000000000")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestUsersMarkLoginUpdatesTimestamp(t *testing.T) {
	s := newTestStore(t, "users_marklogin")
	ctx := context.Background()

	u, err := s.Users().UpsertOAuth(ctx, OAuthProfile{Provider: "google", Subject: "g1", Email: "x@y"})
	require.NoError(t, err)
	prev := u.LastLoginAt

	time.Sleep(5 * time.Millisecond)
	require.NoError(t, s.Users().MarkLogin(ctx, u.ID))

	got, err := s.Users().GetByID(ctx, u.ID)
	require.NoError(t, err)
	require.True(t, got.LastLoginAt.After(prev))
}

func TestUsersSetKeepTranscripts(t *testing.T) {
	s := newTestStore(t, "users_keep")
	ctx := context.Background()

	u, err := s.Users().UpsertOAuth(ctx, OAuthProfile{Provider: "github", Subject: "k1"})
	require.NoError(t, err)
	require.False(t, u.KeepTranscripts)

	require.NoError(t, s.Users().SetKeepTranscripts(ctx, u.ID, true))
	got, err := s.Users().GetByID(ctx, u.ID)
	require.NoError(t, err)
	require.True(t, got.KeepTranscripts)
}
```

- [ ] **Step 2: Add a per-test store helper**

Append to `internal/store/testhelpers.go`:

```go
import "fmt"

// newTestStore returns a store backed by a fresh database name (so tests
// don't share state) on the shared mongo container.
func newTestStore(t *testing.T, label string) *Store {
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
```

(merge the new `import "fmt"` into the existing import block.)

- [ ] **Step 3: Run test to verify it fails**

```bash
go test ./internal/store/... -run Users -timeout 90s
```

Expected: compile failure — `undefined: OAuthProfile`, `undefined: ErrNotFound`, `(*Store).Users undefined`.

- [ ] **Step 4: Implement users.go**

`internal/store/users.go`:

```go
package store

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

var ErrNotFound = errors.New("store: not found")

// User is the persisted shape of an end-user account.
type User struct {
	ID              string    `bson:"_id,omitempty"`
	OAuthProvider   string    `bson:"oauth_provider"`
	OAuthSubject    string    `bson:"oauth_subject"`
	Email           string    `bson:"email,omitempty"`
	Name            string    `bson:"name,omitempty"`
	AvatarURL       string    `bson:"avatar_url,omitempty"`
	KeepTranscripts bool      `bson:"keep_transcripts"`
	CreatedAt       time.Time `bson:"created_at"`
	LastLoginAt     time.Time `bson:"last_login_at"`
}

// OAuthProfile is what an OAuth provider gives us after callback.
type OAuthProfile struct {
	Provider  string
	Subject   string
	Email     string
	Name      string
	AvatarURL string
}

type UsersRepo struct{ coll *mongo.Collection }

func (s *Store) Users() *UsersRepo { return &UsersRepo{coll: s.db.Collection("users")} }

// UpsertOAuth inserts a new user if (provider, subject) is unseen, or
// updates the profile fields (email/name/avatar) and last_login_at if seen.
func (r *UsersRepo) UpsertOAuth(ctx context.Context, p OAuthProfile) (*User, error) {
	now := time.Now().UTC()
	filter := bson.M{"oauth_provider": p.Provider, "oauth_subject": p.Subject}
	update := bson.M{
		"$set": bson.M{
			"email":         p.Email,
			"name":          p.Name,
			"avatar_url":    p.AvatarURL,
			"last_login_at": now,
		},
		"$setOnInsert": bson.M{
			"oauth_provider":   p.Provider,
			"oauth_subject":    p.Subject,
			"keep_transcripts": false,
			"created_at":       now,
		},
	}
	opts := options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After)
	var u User
	if err := r.coll.FindOneAndUpdate(ctx, filter, update, opts).Decode(&u); err != nil {
		return nil, err
	}
	return &u, nil
}

func (r *UsersRepo) GetByID(ctx context.Context, id string) (*User, error) {
	var u User
	err := r.coll.FindOne(ctx, bson.M{"_id": objectIDFromHex(id)}).Decode(&u)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	return &u, err
}

func (r *UsersRepo) MarkLogin(ctx context.Context, id string) error {
	_, err := r.coll.UpdateByID(ctx, objectIDFromHex(id), bson.M{
		"$set": bson.M{"last_login_at": time.Now().UTC()},
	})
	return err
}

func (r *UsersRepo) SetKeepTranscripts(ctx context.Context, id string, v bool) error {
	_, err := r.coll.UpdateByID(ctx, objectIDFromHex(id), bson.M{
		"$set": bson.M{"keep_transcripts": v},
	})
	return err
}
```

- [ ] **Step 5: Add `objectIDFromHex` helper**

Append to `internal/store/store.go`:

```go
import "go.mongodb.org/mongo-driver/v2/bson"

// objectIDFromHex converts a 24-hex-char string into a Mongo ObjectID.
// Returns the zero ObjectID if hex is empty or malformed; callers that
// care should validate first via bson.ObjectIDFromHex.
func objectIDFromHex(hex string) bson.ObjectID {
	id, _ := bson.ObjectIDFromHex(hex)
	return id
}
```

(merge the import).

- [ ] **Step 6: Run tests**

```bash
go test ./internal/store/... -timeout 90s
```

Expected: 4 user tests + earlier 2 store tests all PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/store
git commit -m "feat(store): users repo with OAuth upsert + GetByID + MarkLogin"
```

---

## Task 4: Wrappers + access_tokens repos

**Goal:** Persist paired wrappers and short-lived access tokens. Refresh tokens stored as bcrypt; access tokens stored as sha256 with TTL.

**Files:**
- Create: `internal/store/wrappers.go`
- Create: `internal/store/wrapper_tokens.go`
- Create: `internal/store/wrappers_test.go`

- [ ] **Step 1: Add bcrypt dependency**

```bash
go get golang.org/x/crypto/bcrypt
```

- [ ] **Step 2: Write failing test**

`internal/store/wrappers_test.go`:

```go
package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWrappersInsertAndList(t *testing.T) {
	s := newTestStore(t, "wrappers_basic")
	ctx := context.Background()

	u, err := s.Users().UpsertOAuth(ctx, OAuthProfile{Provider: "github", Subject: "u1"})
	require.NoError(t, err)

	w, plain, err := s.Wrappers().Create(ctx, WrapperCreate{
		UserID: u.ID, Name: "ireland", OS: "linux", Arch: "amd64", Version: "0.1.0",
	})
	require.NoError(t, err)
	require.NotEmpty(t, w.ID)
	require.NotEmpty(t, plain) // refresh token returned to wrapper

	list, err := s.Wrappers().ListByUser(ctx, u.ID)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, "ireland", list[0].Name)
}

func TestWrappersVerifyRefreshToken(t *testing.T) {
	s := newTestStore(t, "wrappers_refresh")
	ctx := context.Background()

	u, _ := s.Users().UpsertOAuth(ctx, OAuthProfile{Provider: "github", Subject: "u2"})
	w, plain, err := s.Wrappers().Create(ctx, WrapperCreate{UserID: u.ID, Name: "x", OS: "linux", Arch: "amd64"})
	require.NoError(t, err)

	got, err := s.Wrappers().VerifyRefreshToken(ctx, plain)
	require.NoError(t, err)
	require.Equal(t, w.ID, got.ID)

	// Wrong token rejected.
	_, err = s.Wrappers().VerifyRefreshToken(ctx, plain+"tamper")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestWrappersRevokedRejectsVerify(t *testing.T) {
	s := newTestStore(t, "wrappers_revoked")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, OAuthProfile{Provider: "github", Subject: "u3"})
	w, plain, _ := s.Wrappers().Create(ctx, WrapperCreate{UserID: u.ID, Name: "x", OS: "linux", Arch: "amd64"})

	require.NoError(t, s.Wrappers().Revoke(ctx, w.ID))
	_, err := s.Wrappers().VerifyRefreshToken(ctx, plain)
	require.ErrorIs(t, err, ErrRevoked)
}

func TestWrapperAccessTokenLifecycle(t *testing.T) {
	s := newTestStore(t, "wrappers_access")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, OAuthProfile{Provider: "github", Subject: "u4"})
	w, _, _ := s.Wrappers().Create(ctx, WrapperCreate{UserID: u.ID, Name: "x", OS: "linux", Arch: "amd64"})

	plain, expiresAt, err := s.WrapperTokens().Issue(ctx, w.ID, u.ID, time.Hour)
	require.NoError(t, err)
	require.NotEmpty(t, plain)
	require.True(t, expiresAt.After(time.Now()))

	got, err := s.WrapperTokens().Verify(ctx, plain)
	require.NoError(t, err)
	require.Equal(t, w.ID, got.WrapperID)

	// Tampered token fails.
	_, err = s.WrapperTokens().Verify(ctx, plain+"x")
	require.ErrorIs(t, err, ErrNotFound)
}
```

- [ ] **Step 3: Run test to verify it fails**

```bash
go test ./internal/store/... -run Wrapper -timeout 90s
```

Expected: compile failures.

- [ ] **Step 4: Implement wrappers.go**

`internal/store/wrappers.go`:

```go
package store

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"golang.org/x/crypto/bcrypt"
)

var ErrRevoked = errors.New("store: wrapper revoked")

type Wrapper struct {
	ID                string     `bson:"_id,omitempty"`
	UserID            string     `bson:"user_id"`
	Name              string     `bson:"name"`
	OS                string     `bson:"os"`
	Arch              string     `bson:"arch"`
	Version           string     `bson:"version"`
	PairedAt          time.Time  `bson:"paired_at"`
	LastSeenAt        time.Time  `bson:"last_seen_at"`
	RefreshTokenHash  string     `bson:"refresh_token_hash"`
	RefreshTokenID    string     `bson:"refresh_token_id"`
	RevokedAt         *time.Time `bson:"revoked_at,omitempty"`
}

type WrapperCreate struct {
	UserID  string
	Name    string
	OS      string
	Arch    string
	Version string
}

type WrappersRepo struct{ coll *mongo.Collection }

func (s *Store) Wrappers() *WrappersRepo { return &WrappersRepo{coll: s.db.Collection("wrappers")} }

// Create inserts a new wrapper row and returns the (wrapper, plaintext refresh
// token) pair. The plaintext is sent to the wrapper once and never persisted.
func (r *WrappersRepo) Create(ctx context.Context, in WrapperCreate) (*Wrapper, string, error) {
	tokenID := ulid.Make().String()
	rnd, err := randomToken(48)
	if err != nil {
		return nil, "", err
	}
	plain := tokenID + "." + rnd
	hash, err := bcrypt.GenerateFromPassword([]byte(rnd), bcrypt.DefaultCost)
	if err != nil {
		return nil, "", err
	}
	now := time.Now().UTC()
	doc := bson.M{
		"_id":                bson.NewObjectID(),
		"user_id":            objectIDFromHex(in.UserID),
		"name":               in.Name,
		"os":                 in.OS,
		"arch":               in.Arch,
		"version":            in.Version,
		"paired_at":          now,
		"last_seen_at":       now,
		"refresh_token_id":   tokenID,
		"refresh_token_hash": string(hash),
	}
	res, err := r.coll.InsertOne(ctx, doc)
	if err != nil {
		return nil, "", err
	}
	w := &Wrapper{
		ID:               res.InsertedID.(bson.ObjectID).Hex(),
		UserID:           in.UserID,
		Name:             in.Name,
		OS:               in.OS,
		Arch:             in.Arch,
		Version:          in.Version,
		PairedAt:         now,
		LastSeenAt:       now,
		RefreshTokenID:   tokenID,
		RefreshTokenHash: string(hash),
	}
	return w, plain, nil
}

func (r *WrappersRepo) ListByUser(ctx context.Context, userID string) ([]Wrapper, error) {
	cur, err := r.coll.Find(ctx, bson.M{"user_id": objectIDFromHex(userID)})
	if err != nil {
		return nil, err
	}
	var out []Wrapper
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// VerifyRefreshToken parses "<token_id>.<random>", looks up the row, and
// bcrypt-compares the random part. Returns ErrRevoked if revoked, ErrNotFound
// if no match.
func (r *WrappersRepo) VerifyRefreshToken(ctx context.Context, plain string) (*Wrapper, error) {
	parts := strings.SplitN(plain, ".", 2)
	if len(parts) != 2 {
		return nil, ErrNotFound
	}
	tokenID, rnd := parts[0], parts[1]
	var w Wrapper
	if err := r.coll.FindOne(ctx, bson.M{"refresh_token_id": tokenID}).Decode(&w); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if w.RevokedAt != nil {
		return nil, ErrRevoked
	}
	if err := bcrypt.CompareHashAndPassword([]byte(w.RefreshTokenHash), []byte(rnd)); err != nil {
		return nil, ErrNotFound
	}
	return &w, nil
}

func (r *WrappersRepo) Revoke(ctx context.Context, id string) error {
	now := time.Now().UTC()
	_, err := r.coll.UpdateByID(ctx, objectIDFromHex(id), bson.M{"$set": bson.M{"revoked_at": now}})
	return err
}

func (r *WrappersRepo) UpdateLastSeen(ctx context.Context, id string) error {
	_, err := r.coll.UpdateByID(ctx, objectIDFromHex(id), bson.M{
		"$set": bson.M{"last_seen_at": time.Now().UTC()},
	})
	return err
}

func randomToken(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
```

- [ ] **Step 5: Implement wrapper_tokens.go**

`internal/store/wrapper_tokens.go`:

```go
package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

type WrapperAccessToken struct {
	ID        string    `bson:"_id,omitempty"`
	WrapperID string    `bson:"wrapper_id"`
	UserID    string    `bson:"user_id"`
	TokenHash string    `bson:"token_hash"`
	ExpiresAt time.Time `bson:"expires_at"`
}

type WrapperTokensRepo struct{ coll *mongo.Collection }

func (s *Store) WrapperTokens() *WrapperTokensRepo {
	return &WrapperTokensRepo{coll: s.db.Collection("wrapper_access_tokens")}
}

// Issue creates a new access token for a wrapper and returns the plaintext
// (sent to the wrapper once) plus expiry.
func (r *WrapperTokensRepo) Issue(ctx context.Context, wrapperID, userID string, ttl time.Duration) (string, time.Time, error) {
	plain, err := randomToken(32)
	if err != nil {
		return "", time.Time{}, err
	}
	expiresAt := time.Now().UTC().Add(ttl)
	_, err = r.coll.InsertOne(ctx, bson.M{
		"wrapper_id": objectIDFromHex(wrapperID),
		"user_id":    objectIDFromHex(userID),
		"token_hash": hashToken(plain),
		"expires_at": expiresAt,
	})
	if err != nil {
		return "", time.Time{}, err
	}
	return plain, expiresAt, nil
}

// Verify looks the token up by sha256 hash. Returns ErrNotFound if absent
// or expired (Mongo's TTL background sweeper deletes lazily, so we double-check).
func (r *WrapperTokensRepo) Verify(ctx context.Context, plain string) (*WrapperAccessToken, error) {
	var t WrapperAccessToken
	err := r.coll.FindOne(ctx, bson.M{"token_hash": hashToken(plain)}).Decode(&t)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if time.Now().UTC().After(t.ExpiresAt) {
		return nil, ErrNotFound
	}
	return &t, nil
}

func hashToken(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}
```

- [ ] **Step 6: Run tests**

```bash
go test ./internal/store/... -timeout 120s
```

Expected: all wrapper tests PASS plus prior tests.

- [ ] **Step 7: Commit**

```bash
git add internal/store go.mod go.sum
git commit -m "feat(store): wrappers + wrapper_access_tokens repos"
```

---

## Task 5: Pairing codes repo

**Goal:** `pairing_codes` CRUD: Create with TTL, FindByCode, Approve(setting user_id+status), GetByCode for poll.

**Files:**
- Create: `internal/store/pairing.go`
- Create: `internal/store/pairing_test.go`

- [ ] **Step 1: Write failing test**

`internal/store/pairing_test.go`:

```go
package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPairingCreateAndGet(t *testing.T) {
	s := newTestStore(t, "pair_create")
	ctx := context.Background()

	pc, err := s.Pairing().Create(ctx, WrapperDescriptor{
		Name: "ireland", OS: "linux", Arch: "amd64", Version: "0.1.0",
	}, 10*time.Minute)
	require.NoError(t, err)
	require.Len(t, pc.Code, 9) // ABCD-1234
	require.Equal(t, "pending", pc.Status)

	got, err := s.Pairing().GetByCode(ctx, pc.Code)
	require.NoError(t, err)
	require.Equal(t, pc.Code, got.Code)
}

func TestPairingApproveSetsUserAndStatus(t *testing.T) {
	s := newTestStore(t, "pair_approve")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, OAuthProfile{Provider: "github", Subject: "u1"})
	pc, _ := s.Pairing().Create(ctx, WrapperDescriptor{Name: "n", OS: "linux", Arch: "amd64"}, time.Minute)

	require.NoError(t, s.Pairing().Approve(ctx, pc.Code, u.ID))

	got, _ := s.Pairing().GetByCode(ctx, pc.Code)
	require.Equal(t, "approved", got.Status)
	require.Equal(t, u.ID, got.UserID)
}

func TestPairingDeleteAfterRedeem(t *testing.T) {
	s := newTestStore(t, "pair_delete")
	ctx := context.Background()
	pc, _ := s.Pairing().Create(ctx, WrapperDescriptor{Name: "n", OS: "linux", Arch: "amd64"}, time.Minute)

	require.NoError(t, s.Pairing().Delete(ctx, pc.Code))

	_, err := s.Pairing().GetByCode(ctx, pc.Code)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestPairingDoubleApproveIsError(t *testing.T) {
	s := newTestStore(t, "pair_double")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, OAuthProfile{Provider: "github", Subject: "u2"})
	pc, _ := s.Pairing().Create(ctx, WrapperDescriptor{Name: "n", OS: "linux", Arch: "amd64"}, time.Minute)

	require.NoError(t, s.Pairing().Approve(ctx, pc.Code, u.ID))
	err := s.Pairing().Approve(ctx, pc.Code, u.ID)
	require.ErrorIs(t, err, ErrAlreadyApproved)
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/store/... -run Pairing -timeout 90s
```

Expected: compile failure.

- [ ] **Step 3: Implement pairing.go**

`internal/store/pairing.go`:

```go
package store

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

var ErrAlreadyApproved = errors.New("store: pairing already approved")

type WrapperDescriptor struct {
	Name    string `bson:"name"`
	OS      string `bson:"os"`
	Arch    string `bson:"arch"`
	Version string `bson:"version"`
}

type PairingCode struct {
	ID        string             `bson:"_id,omitempty"`
	Code      string             `bson:"code"`
	Status    string             `bson:"status"` // pending | approved | denied
	UserID    string             `bson:"user_id,omitempty"`
	Wrapper   WrapperDescriptor  `bson:"wrapper"`
	ExpiresAt time.Time          `bson:"expires_at"`
}

type PairingRepo struct{ coll *mongo.Collection }

func (s *Store) Pairing() *PairingRepo { return &PairingRepo{coll: s.db.Collection("pairing_codes")} }

func (r *PairingRepo) Create(ctx context.Context, w WrapperDescriptor, ttl time.Duration) (*PairingCode, error) {
	code, err := generatePairingCode()
	if err != nil {
		return nil, err
	}
	pc := &PairingCode{
		Code:      code,
		Status:    "pending",
		Wrapper:   w,
		ExpiresAt: time.Now().UTC().Add(ttl),
	}
	res, err := r.coll.InsertOne(ctx, pc)
	if err != nil {
		return nil, err
	}
	pc.ID = res.InsertedID.(bson.ObjectID).Hex()
	return pc, nil
}

func (r *PairingRepo) GetByCode(ctx context.Context, code string) (*PairingCode, error) {
	var pc PairingCode
	err := r.coll.FindOne(ctx, bson.M{"code": code}).Decode(&pc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	return &pc, err
}

func (r *PairingRepo) Approve(ctx context.Context, code, userID string) error {
	res, err := r.coll.UpdateOne(ctx,
		bson.M{"code": code, "status": "pending"},
		bson.M{"$set": bson.M{"status": "approved", "user_id": objectIDFromHex(userID)}},
	)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		// Either not found, or status != pending.
		var pc PairingCode
		if err := r.coll.FindOne(ctx, bson.M{"code": code}).Decode(&pc); err == nil {
			return ErrAlreadyApproved
		}
		return ErrNotFound
	}
	return nil
}

func (r *PairingRepo) Delete(ctx context.Context, code string) error {
	_, err := r.coll.DeleteOne(ctx, bson.M{"code": code})
	return err
}

// generatePairingCode produces "ABCD-1234" style codes, uppercase A-Z and
// digits, with no ambiguous chars (0/O/1/I/L excluded).
func generatePairingCode() (string, error) {
	const alphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
	pick := func(n int) (string, error) {
		buf := make([]byte, n)
		bytes := make([]byte, n)
		if _, err := rand.Read(bytes); err != nil {
			return "", err
		}
		for i, b := range bytes {
			buf[i] = alphabet[int(b)%len(alphabet)]
		}
		return string(buf), nil
	}
	a, err := pick(4)
	if err != nil {
		return "", err
	}
	b, err := pick(4)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%s", a, b), nil
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/store/... -timeout 120s
```

Expected: 4 pairing tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store
git commit -m "feat(store): pairing_codes repo with create+approve+delete"
```

---

## Task 6: Sessions repo

**Goal:** `sessions` collection CRUD with status transitions.

**Files:**
- Create: `internal/store/sessions.go`
- Create: `internal/store/sessions_test.go`

- [ ] **Step 1: Write failing test**

`internal/store/sessions_test.go`:

```go
package store

import (
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"
)

func TestSessionsCreateAndGet(t *testing.T) {
	s := newTestStore(t, "sessions_basic")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, OAuthProfile{Provider: "github", Subject: "u1"})
	w, _, _ := s.Wrappers().Create(ctx, WrapperCreate{UserID: u.ID, Name: "x", OS: "linux", Arch: "amd64"})

	sid := ulid.Make().String()
	sess, err := s.Sessions().Create(ctx, SessionCreate{
		ID: sid, UserID: u.ID, WrapperID: w.ID, Cwd: "/tmp", Account: "default",
	})
	require.NoError(t, err)
	require.Equal(t, sid, sess.ID)
	require.Equal(t, "starting", sess.Status)

	got, err := s.Sessions().GetByID(ctx, sid)
	require.NoError(t, err)
	require.Equal(t, "/tmp", got.Cwd)
}

func TestSessionsTransitions(t *testing.T) {
	s := newTestStore(t, "sessions_xitions")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, OAuthProfile{Provider: "github", Subject: "u2"})
	w, _, _ := s.Wrappers().Create(ctx, WrapperCreate{UserID: u.ID, Name: "x", OS: "linux", Arch: "amd64"})
	sid := ulid.Make().String()
	_, err := s.Sessions().Create(ctx, SessionCreate{ID: sid, UserID: u.ID, WrapperID: w.ID, Cwd: "/", Account: "default"})
	require.NoError(t, err)

	require.NoError(t, s.Sessions().MarkRunning(ctx, sid, "abcd1234"))
	got, _ := s.Sessions().GetByID(ctx, sid)
	require.Equal(t, "running", got.Status)
	require.Equal(t, "abcd1234", got.JSONLUUID)

	require.NoError(t, s.Sessions().MarkExited(ctx, sid, 0, "normal", ""))
	got, _ = s.Sessions().GetByID(ctx, sid)
	require.Equal(t, "exited", got.Status)
	require.NotNil(t, got.ExitCode)
	require.Equal(t, 0, *got.ExitCode)
}

func TestSessionsListByUser(t *testing.T) {
	s := newTestStore(t, "sessions_list")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, OAuthProfile{Provider: "github", Subject: "u3"})
	w, _, _ := s.Wrappers().Create(ctx, WrapperCreate{UserID: u.ID, Name: "x", OS: "linux", Arch: "amd64"})

	for i := 0; i < 3; i++ {
		_, err := s.Sessions().Create(ctx, SessionCreate{
			ID: ulid.Make().String(), UserID: u.ID, WrapperID: w.ID,
			Cwd: "/", Account: "default",
		})
		require.NoError(t, err)
	}

	got, err := s.Sessions().ListByUser(ctx, u.ID, "")
	require.NoError(t, err)
	require.Len(t, got, 3)
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/store/... -run Sessions -timeout 90s
```

Expected: compile failure.

- [ ] **Step 3: Implement sessions.go**

`internal/store/sessions.go`:

```go
package store

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type Session struct {
	ID         string     `bson:"_id"`
	UserID     string     `bson:"user_id"`
	WrapperID  string     `bson:"wrapper_id"`
	JSONLUUID  string     `bson:"jsonl_uuid,omitempty"`
	Cwd        string     `bson:"cwd"`
	Account    string     `bson:"account"`
	Status     string     `bson:"status"` // starting | running | exited | wrapper_offline
	CreatedAt  time.Time  `bson:"created_at"`
	ExitedAt   *time.Time `bson:"exited_at,omitempty"`
	ExitCode   *int       `bson:"exit_code,omitempty"`
	ExitReason string     `bson:"exit_reason,omitempty"`
}

type SessionCreate struct {
	ID        string
	UserID    string
	WrapperID string
	Cwd       string
	Account   string
}

type SessionsRepo struct{ coll *mongo.Collection }

func (s *Store) Sessions() *SessionsRepo { return &SessionsRepo{coll: s.db.Collection("sessions")} }

func (r *SessionsRepo) Create(ctx context.Context, in SessionCreate) (*Session, error) {
	now := time.Now().UTC()
	doc := bson.M{
		"_id":        in.ID,
		"user_id":    objectIDFromHex(in.UserID),
		"wrapper_id": objectIDFromHex(in.WrapperID),
		"cwd":        in.Cwd,
		"account":    in.Account,
		"status":     "starting",
		"created_at": now,
	}
	if _, err := r.coll.InsertOne(ctx, doc); err != nil {
		return nil, err
	}
	return &Session{
		ID: in.ID, UserID: in.UserID, WrapperID: in.WrapperID,
		Cwd: in.Cwd, Account: in.Account, Status: "starting", CreatedAt: now,
	}, nil
}

func (r *SessionsRepo) GetByID(ctx context.Context, id string) (*Session, error) {
	var s Session
	err := r.coll.FindOne(ctx, bson.M{"_id": id}).Decode(&s)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	return &s, err
}

func (r *SessionsRepo) MarkRunning(ctx context.Context, id, jsonlUUID string) error {
	_, err := r.coll.UpdateOne(ctx, bson.M{"_id": id}, bson.M{
		"$set": bson.M{"status": "running", "jsonl_uuid": jsonlUUID},
	})
	return err
}

func (r *SessionsRepo) MarkExited(ctx context.Context, id string, exitCode int, reason, detail string) error {
	now := time.Now().UTC()
	_, err := r.coll.UpdateOne(ctx, bson.M{"_id": id}, bson.M{
		"$set": bson.M{
			"status":      "exited",
			"exited_at":   now,
			"exit_code":   exitCode,
			"exit_reason": reason,
		},
	})
	return err
}

func (r *SessionsRepo) MarkWrapperOffline(ctx context.Context, wrapperID string) (int64, error) {
	res, err := r.coll.UpdateMany(ctx,
		bson.M{"wrapper_id": objectIDFromHex(wrapperID), "status": bson.M{"$in": []string{"starting", "running"}}},
		bson.M{"$set": bson.M{"status": "wrapper_offline"}},
	)
	if err != nil {
		return 0, err
	}
	return res.ModifiedCount, nil
}

func (r *SessionsRepo) MarkRunningFromOffline(ctx context.Context, id string) error {
	_, err := r.coll.UpdateOne(ctx,
		bson.M{"_id": id, "status": "wrapper_offline"},
		bson.M{"$set": bson.M{"status": "running"}},
	)
	return err
}

// ListByUser returns sessions for a user, optionally filtered by status.
// statusFilter "" returns all; "live" returns running+starting+wrapper_offline;
// "exited" returns just exited.
func (r *SessionsRepo) ListByUser(ctx context.Context, userID, statusFilter string) ([]Session, error) {
	filter := bson.M{"user_id": objectIDFromHex(userID)}
	switch statusFilter {
	case "live":
		filter["status"] = bson.M{"$in": []string{"starting", "running", "wrapper_offline"}}
	case "exited":
		filter["status"] = "exited"
	}
	cur, err := r.coll.Find(ctx, filter, options.Find().SetSort(bson.M{"created_at": -1}))
	if err != nil {
		return nil, err
	}
	var out []Session
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/store/... -timeout 120s
```

Expected: 3 session tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store
git commit -m "feat(store): sessions repo with status transitions"
```

---

## Task 7: Auth sessions + session messages repos

**Goal:** Browser auth sessions (cookie-keyed) and optional jsonl transcript storage. Both with TTL.

**Files:**
- Create: `internal/store/auth_sessions.go`
- Create: `internal/store/messages.go`
- Create: `internal/store/auth_sessions_test.go`
- Create: `internal/store/messages_test.go`

- [ ] **Step 1: Write failing tests**

`internal/store/auth_sessions_test.go`:

```go
package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAuthSessionsCreateAndGet(t *testing.T) {
	s := newTestStore(t, "authsess_basic")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, OAuthProfile{Provider: "github", Subject: "u1"})

	sess, err := s.AuthSessions().Create(ctx, u.ID, 30*24*time.Hour)
	require.NoError(t, err)
	require.NotEmpty(t, sess.ID)
	require.NotEmpty(t, sess.CSRFToken)

	got, err := s.AuthSessions().GetByID(ctx, sess.ID)
	require.NoError(t, err)
	require.Equal(t, u.ID, got.UserID)
}

func TestAuthSessionsDeleteRevokes(t *testing.T) {
	s := newTestStore(t, "authsess_delete")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, OAuthProfile{Provider: "github", Subject: "u2"})
	sess, _ := s.AuthSessions().Create(ctx, u.ID, time.Hour)

	require.NoError(t, s.AuthSessions().Delete(ctx, sess.ID))
	_, err := s.AuthSessions().GetByID(ctx, sess.ID)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestAuthSessionsTouchExtendsExpiry(t *testing.T) {
	s := newTestStore(t, "authsess_touch")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, OAuthProfile{Provider: "github", Subject: "u3"})
	sess, _ := s.AuthSessions().Create(ctx, u.ID, time.Minute)

	time.Sleep(5 * time.Millisecond)
	require.NoError(t, s.AuthSessions().Touch(ctx, sess.ID, time.Hour))

	got, _ := s.AuthSessions().GetByID(ctx, sess.ID)
	require.True(t, got.ExpiresAt.Sub(time.Now()) > 30*time.Minute)
}
```

`internal/store/messages_test.go`:

```go
package store

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"
)

func TestMessagesAppendAndList(t *testing.T) {
	s := newTestStore(t, "messages_basic")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, OAuthProfile{Provider: "github", Subject: "u1"})
	sid := ulid.Make().String()

	for i := 0; i < 3; i++ {
		require.NoError(t, s.Messages().Append(ctx, sid, u.ID, time.Now(), "line "+string(rune('a'+i))))
	}

	out, err := s.Messages().List(ctx, sid, time.Time{}, 10)
	require.NoError(t, err)
	require.Len(t, out, 3)
	require.Equal(t, "line a", out[0].Entry)
}

func TestMessagesListSinceFilters(t *testing.T) {
	s := newTestStore(t, "messages_since")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, OAuthProfile{Provider: "github", Subject: "u2"})
	sid := ulid.Make().String()

	t0 := time.Now().UTC()
	require.NoError(t, s.Messages().Append(ctx, sid, u.ID, t0, "old"))
	require.NoError(t, s.Messages().Append(ctx, sid, u.ID, t0.Add(time.Second), "new"))

	out, err := s.Messages().List(ctx, sid, t0.Add(500*time.Millisecond), 10)
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Equal(t, "new", out[0].Entry)
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/store/... -run "AuthSessions|Messages" -timeout 90s
```

Expected: compile failure.

- [ ] **Step 3: Implement auth_sessions.go**

`internal/store/auth_sessions.go`:

```go
package store

import (
	"context"
	"errors"
	"time"

	"github.com/oklog/ulid/v2"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

type AuthSession struct {
	ID        string    `bson:"_id"`
	UserID    string    `bson:"user_id"`
	CSRFToken string    `bson:"csrf_token"`
	CreatedAt time.Time `bson:"created_at"`
	LastSeen  time.Time `bson:"last_seen"`
	ExpiresAt time.Time `bson:"expires_at"`
}

type AuthSessionsRepo struct{ coll *mongo.Collection }

func (s *Store) AuthSessions() *AuthSessionsRepo {
	return &AuthSessionsRepo{coll: s.db.Collection("auth_sessions")}
}

func (r *AuthSessionsRepo) Create(ctx context.Context, userID string, ttl time.Duration) (*AuthSession, error) {
	csrf, err := randomToken(24)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	sess := &AuthSession{
		ID:        ulid.Make().String(),
		UserID:    userID,
		CSRFToken: csrf,
		CreatedAt: now,
		LastSeen:  now,
		ExpiresAt: now.Add(ttl),
	}
	doc := bson.M{
		"_id":        sess.ID,
		"user_id":    objectIDFromHex(userID),
		"csrf_token": sess.CSRFToken,
		"created_at": sess.CreatedAt,
		"last_seen":  sess.LastSeen,
		"expires_at": sess.ExpiresAt,
	}
	if _, err := r.coll.InsertOne(ctx, doc); err != nil {
		return nil, err
	}
	return sess, nil
}

func (r *AuthSessionsRepo) GetByID(ctx context.Context, id string) (*AuthSession, error) {
	var sess AuthSession
	err := r.coll.FindOne(ctx, bson.M{"_id": id}).Decode(&sess)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if time.Now().UTC().After(sess.ExpiresAt) {
		return nil, ErrNotFound
	}
	return &sess, nil
}

func (r *AuthSessionsRepo) Delete(ctx context.Context, id string) error {
	_, err := r.coll.DeleteOne(ctx, bson.M{"_id": id})
	return err
}

func (r *AuthSessionsRepo) Touch(ctx context.Context, id string, ttl time.Duration) error {
	now := time.Now().UTC()
	_, err := r.coll.UpdateByID(ctx, id, bson.M{
		"$set": bson.M{"last_seen": now, "expires_at": now.Add(ttl)},
	})
	return err
}
```

- [ ] **Step 4: Implement messages.go**

`internal/store/messages.go`:

```go
package store

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type SessionMessage struct {
	ID        string    `bson:"_id,omitempty"`
	SessionID string    `bson:"session_id"`
	UserID    string    `bson:"user_id"`
	TS        time.Time `bson:"ts"`
	Entry     string    `bson:"entry"`
}

type MessagesRepo struct{ coll *mongo.Collection }

func (s *Store) Messages() *MessagesRepo {
	return &MessagesRepo{coll: s.db.Collection("session_messages")}
}

func (r *MessagesRepo) Append(ctx context.Context, sessionID, userID string, ts time.Time, entry string) error {
	_, err := r.coll.InsertOne(ctx, bson.M{
		"session_id": sessionID,
		"user_id":    objectIDFromHex(userID),
		"ts":         ts.UTC(),
		"entry":      entry,
	})
	return err
}

// List returns messages for a session, ascending by ts. since == zero means
// from the beginning. limit caps the number of returned rows.
func (r *MessagesRepo) List(ctx context.Context, sessionID string, since time.Time, limit int) ([]SessionMessage, error) {
	filter := bson.M{"session_id": sessionID}
	if !since.IsZero() {
		filter["ts"] = bson.M{"$gt": since.UTC()}
	}
	opts := options.Find().SetSort(bson.M{"ts": 1}).SetLimit(int64(limit))
	cur, err := r.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	var out []SessionMessage
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}
```

- [ ] **Step 5: Run tests**

```bash
go test ./internal/store/... -timeout 120s
```

Expected: all store tests PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/store
git commit -m "feat(store): auth_sessions + session_messages repos"
```

---

## Task 8: CSRF helper

**Goal:** Cookie-issued CSRF token; double-submit verification on mutating requests.

**Files:**
- Create: `internal/csrf/csrf.go`
- Create: `internal/csrf/csrf_test.go`

- [ ] **Step 1: Write failing test**

`internal/csrf/csrf_test.go`:

```go
package csrf

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVerifyAcceptsMatching(t *testing.T) {
	req := httptest.NewRequest("POST", "/x", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: "abc"})
	req.Header.Set(HeaderName, "abc")

	require.NoError(t, Verify(req))
}

func TestVerifyRejectsMissingCookie(t *testing.T) {
	req := httptest.NewRequest("POST", "/x", nil)
	req.Header.Set(HeaderName, "abc")

	err := Verify(req)
	require.ErrorIs(t, err, ErrMissingCookie)
}

func TestVerifyRejectsMissingHeader(t *testing.T) {
	req := httptest.NewRequest("POST", "/x", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: "abc"})

	err := Verify(req)
	require.ErrorIs(t, err, ErrMissingHeader)
}

func TestVerifyRejectsMismatch(t *testing.T) {
	req := httptest.NewRequest("POST", "/x", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: "abc"})
	req.Header.Set(HeaderName, "xyz")

	err := Verify(req)
	require.ErrorIs(t, err, ErrMismatch)
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/csrf/...
```

Expected: undefined symbols.

- [ ] **Step 3: Implement csrf.go**

`internal/csrf/csrf.go`:

```go
// Package csrf implements double-submit cookie protection.
// Cookie is non-HttpOnly so the SPA can read it and mirror its value into
// a request header. Server compares; mismatch = reject.
package csrf

import (
	"errors"
	"net/http"
)

const (
	CookieName = "cs_csrf"
	HeaderName = "X-CSRF-Token"
)

var (
	ErrMissingCookie = errors.New("csrf: missing cookie")
	ErrMissingHeader = errors.New("csrf: missing header")
	ErrMismatch      = errors.New("csrf: cookie/header mismatch")
)

// Verify checks the request carries a matching CSRF cookie and header.
// Use only on mutating endpoints (POST/DELETE/PATCH/PUT).
func Verify(r *http.Request) error {
	c, err := r.Cookie(CookieName)
	if err != nil || c.Value == "" {
		return ErrMissingCookie
	}
	h := r.Header.Get(HeaderName)
	if h == "" {
		return ErrMissingHeader
	}
	if c.Value != h {
		return ErrMismatch
	}
	return nil
}

// Set writes the CSRF cookie. token is the auth_session.csrf_token.
func Set(w http.ResponseWriter, token string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: false, // SPA must read it
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/csrf/...
```

Expected: 4 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/csrf
git commit -m "feat(csrf): double-submit cookie verifier"
```

---

## Task 9: Web embed (with noweb stub)

**Goal:** Build-tag-controlled file system that exposes either an embedded SPA bundle or returns 404 in headless mode.

**Files:**
- Create: `internal/webfs/webfs.go`
- Create: `internal/webfs/webfs_noweb.go`
- Create: `internal/webfs/stub/index.html`
- Create: `internal/webfs/webfs_test.go`

- [ ] **Step 1: Write failing test**

`internal/webfs/webfs_test.go`:

```go
//go:build !noweb

package webfs

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHandlerServesStubIndex(t *testing.T) {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	Handler().ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)

	body, _ := io.ReadAll(rr.Body)
	require.True(t, strings.Contains(string(body), "claude-switch"))
}

func TestHandlerSpaFallback(t *testing.T) {
	// Unknown SPA route should still return index.html (so client-side
	// routing works without a matching server route).
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/some/spa/route", nil)
	Handler().ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)
}
```

- [ ] **Step 2: Create stub index.html**

`internal/webfs/stub/index.html`:

```html
<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <title>claude-switch</title>
    <meta name="viewport" content="width=device-width, initial-scale=1" />
  </head>
  <body>
    <main>
      <h1>claude-switch</h1>
      <p>The frontend (subsystem 3) is not built into this binary yet.</p>
      <p>This stub is served because the server binary was compiled with
         the embedded webfs but the <code>web/</code> directory was empty.</p>
    </main>
  </body>
</html>
```

- [ ] **Step 3: Run test to verify it fails**

```bash
go test ./internal/webfs/...
```

Expected: undefined `Handler`.

- [ ] **Step 4: Implement webfs.go**

`internal/webfs/webfs.go`:

```go
//go:build !noweb

// Package webfs serves the SPA bundle when present. The bundle is expected
// to be at ../../web (relative to this file's repo root) and is embedded at
// build time. If the web/ directory is empty (subsystem 3 not built yet),
// a small stub index.html is served instead.
package webfs

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed stub/index.html
var stubFS embed.FS

//go:embed all:stub
var bundle embed.FS

// Handler returns an http.Handler that serves the embedded bundle. Routes
// without a matching file fall back to index.html so client-side routing works.
func Handler() http.Handler {
	root, err := fs.Sub(bundle, "stub")
	if err != nil {
		// Should never happen; embed paths are validated at build time.
		panic(err)
	}
	fileSrv := http.FileServer(http.FS(root))
	indexBytes, _ := fs.ReadFile(stubFS, "stub/index.html")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// SPA fallback: if the path looks like an asset (has an extension)
		// serve the file system; otherwise serve index.html.
		if strings.Contains(r.URL.Path, ".") && r.URL.Path != "/" {
			fileSrv.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(indexBytes)
	})
}

// Enabled reports whether the binary embeds a web bundle (true here, false in
// the noweb stub).
func Enabled() bool { return true }
```

When subsystem 3 produces a real bundle (an `index.html` and assets in the `web/` directory at the repo root), this file's `//go:embed all:stub` directive will be replaced with `//go:embed all:../../web` and `fs.Sub(..., "stub")` becomes `fs.Sub(..., "web")`. That swap is a single PR in subsystem 3, isolated from server logic.

- [ ] **Step 5: Implement webfs_noweb.go**

`internal/webfs/webfs_noweb.go`:

```go
//go:build noweb

package webfs

import "net/http"

// Handler returns a handler that 404s every request. Used when the server
// is built with the `noweb` tag (headless API mode).
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
}

// Enabled reports whether the binary embeds a web bundle.
func Enabled() bool { return false }
```

- [ ] **Step 6: Run tests + cross-tag build**

```bash
go test ./internal/webfs/...
go build -tags noweb ./cmd/claude-switch-server
```

Expected: tests PASS; both build modes compile.

- [ ] **Step 7: Commit**

```bash
git add internal/webfs
git commit -m "feat(webfs): embedded SPA stub + noweb headless build tag"
```

---

## Task 10: OAuth provider abstraction + GitHub

**Goal:** Pluggable OAuth provider that takes you from `/auth/<p>/login` (302 with state cookie) to `/auth/<p>/callback` (exchanges code → fetches user profile → returns `OAuthProfile`). GitHub implementation; Google in the next task.

**Files:**
- Create: `internal/oauth/provider.go`
- Create: `internal/oauth/github.go`
- Create: `internal/oauth/github_test.go`

- [ ] **Step 1: Add oauth2 dependency**

```bash
go get golang.org/x/oauth2
```

- [ ] **Step 2: Write failing test for GitHub flow**

`internal/oauth/github_test.go`:

```go
package oauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jleal52/claude-switch/internal/store"
	"github.com/stretchr/testify/require"
)

func TestGitHubExchangeReturnsProfile(t *testing.T) {
	// Fake GitHub OAuth + API.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login/oauth/access_token":
			require.Equal(t, "the-code", r.FormValue("code"))
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "tok-1",
				"token_type":   "bearer",
				"scope":        "user:email",
			})
		case "/user":
			require.Equal(t, "Bearer tok-1", r.Header.Get("Authorization"))
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":         99,
				"login":      "ada",
				"name":       "Ada Lovelace",
				"avatar_url": "https://avatars/x.png",
				"email":      "ada@example.com",
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	gh := NewGitHub(GitHubConfig{
		ClientID: "id", ClientSecret: "secret",
		AuthURL: srv.URL + "/login/oauth/authorize",
		TokenURL: srv.URL + "/login/oauth/access_token",
		APIBase: srv.URL,
	})

	prof, err := gh.Exchange(context.Background(), "the-code")
	require.NoError(t, err)
	require.Equal(t, store.OAuthProfile{
		Provider: "github", Subject: "99", Email: "ada@example.com",
		Name: "Ada Lovelace", AvatarURL: "https://avatars/x.png",
	}, *prof)
}
```

- [ ] **Step 3: Run test to verify it fails**

```bash
go test ./internal/oauth/...
```

Expected: undefined `NewGitHub`, `GitHubConfig`.

- [ ] **Step 4: Implement provider.go**

`internal/oauth/provider.go`:

```go
// Package oauth implements provider-specific OAuth2 callbacks. Each
// provider type implements Provider; the API layer wires them to
// /auth/<name>/login and /auth/<name>/callback.
package oauth

import (
	"context"

	"github.com/jleal52/claude-switch/internal/store"
)

// Provider abstracts a single OAuth2 provider.
type Provider interface {
	// Name returns the lowercase provider identifier ("github", "google").
	Name() string
	// AuthCodeURL returns the URL the browser should be redirected to.
	AuthCodeURL(state string) string
	// Exchange completes the callback: code -> access token -> user profile.
	Exchange(ctx context.Context, code string) (*store.OAuthProfile, error)
}
```

- [ ] **Step 5: Implement github.go**

`internal/oauth/github.go`:

```go
package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"golang.org/x/oauth2"

	"github.com/jleal52/claude-switch/internal/store"
)

type GitHubConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
	// Override URLs (test injection). Empty values use real GitHub.
	AuthURL  string
	TokenURL string
	APIBase  string
}

type GitHub struct {
	cfg     GitHubConfig
	oauthCfg *oauth2.Config
	apiBase string
	hc      *http.Client
}

func NewGitHub(cfg GitHubConfig) *GitHub {
	if cfg.AuthURL == "" {
		cfg.AuthURL = "https://github.com/login/oauth/authorize"
	}
	if cfg.TokenURL == "" {
		cfg.TokenURL = "https://github.com/login/oauth/access_token"
	}
	if cfg.APIBase == "" {
		cfg.APIBase = "https://api.github.com"
	}
	return &GitHub{
		cfg: cfg,
		oauthCfg: &oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURL,
			Endpoint:     oauth2.Endpoint{AuthURL: cfg.AuthURL, TokenURL: cfg.TokenURL},
			Scopes:       []string{"read:user", "user:email"},
		},
		apiBase: cfg.APIBase,
		hc:      &http.Client{Timeout: 15 * time.Second},
	}
}

func (g *GitHub) Name() string { return "github" }

func (g *GitHub) AuthCodeURL(state string) string {
	return g.oauthCfg.AuthCodeURL(state)
}

func (g *GitHub) Exchange(ctx context.Context, code string) (*store.OAuthProfile, error) {
	tok, err := g.oauthCfg.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("github exchange: %w", err)
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, g.apiBase+"/user", nil)
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	req.Header.Set("Accept", "application/json")
	resp, err := g.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("github /user: http %d", resp.StatusCode)
	}
	var u struct {
		ID        int64  `json:"id"`
		Login     string `json:"login"`
		Name      string `json:"name"`
		AvatarURL string `json:"avatar_url"`
		Email     string `json:"email"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return nil, err
	}
	name := u.Name
	if name == "" {
		name = u.Login
	}
	return &store.OAuthProfile{
		Provider:  "github",
		Subject:   strconv.FormatInt(u.ID, 10),
		Email:     u.Email,
		Name:      name,
		AvatarURL: u.AvatarURL,
	}, nil
}
```

- [ ] **Step 6: Run tests**

```bash
go test ./internal/oauth/... -timeout 30s
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/oauth go.mod go.sum
git commit -m "feat(oauth): provider abstraction + GitHub implementation"
```

---

## Task 11: Google OAuth provider

**Goal:** Same `Provider` interface, Google specifics.

**Files:**
- Create: `internal/oauth/google.go`
- Create: `internal/oauth/google_test.go`

- [ ] **Step 1: Write failing test**

`internal/oauth/google_test.go`:

```go
package oauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jleal52/claude-switch/internal/store"
	"github.com/stretchr/testify/require"
)

func TestGoogleExchangeReturnsProfile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "tok-g", "token_type": "Bearer", "expires_in": 3600,
			})
		case "/oauth2/v2/userinfo":
			require.Equal(t, "Bearer tok-g", r.Header.Get("Authorization"))
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":      "g-77",
				"email":   "alan@example.com",
				"name":    "Alan T",
				"picture": "https://gpic/x.jpg",
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	g := NewGoogle(GoogleConfig{
		ClientID: "id", ClientSecret: "s",
		AuthURL: srv.URL + "/auth", TokenURL: srv.URL + "/token",
		UserInfoURL: srv.URL + "/oauth2/v2/userinfo",
	})

	prof, err := g.Exchange(context.Background(), "code")
	require.NoError(t, err)
	require.Equal(t, store.OAuthProfile{
		Provider: "google", Subject: "g-77", Email: "alan@example.com",
		Name: "Alan T", AvatarURL: "https://gpic/x.jpg",
	}, *prof)
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/oauth/...
```

- [ ] **Step 3: Implement google.go**

`internal/oauth/google.go`:

```go
package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"golang.org/x/oauth2"

	"github.com/jleal52/claude-switch/internal/store"
)

type GoogleConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
	AuthURL      string
	TokenURL     string
	UserInfoURL  string
}

type Google struct {
	oauthCfg    *oauth2.Config
	userInfoURL string
	hc          *http.Client
}

func NewGoogle(cfg GoogleConfig) *Google {
	if cfg.AuthURL == "" {
		cfg.AuthURL = "https://accounts.google.com/o/oauth2/v2/auth"
	}
	if cfg.TokenURL == "" {
		cfg.TokenURL = "https://oauth2.googleapis.com/token"
	}
	if cfg.UserInfoURL == "" {
		cfg.UserInfoURL = "https://www.googleapis.com/oauth2/v2/userinfo"
	}
	return &Google{
		oauthCfg: &oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURL,
			Endpoint:     oauth2.Endpoint{AuthURL: cfg.AuthURL, TokenURL: cfg.TokenURL},
			Scopes:       []string{"openid", "email", "profile"},
		},
		userInfoURL: cfg.UserInfoURL,
		hc:          &http.Client{Timeout: 15 * time.Second},
	}
}

func (g *Google) Name() string { return "google" }

func (g *Google) AuthCodeURL(state string) string { return g.oauthCfg.AuthCodeURL(state) }

func (g *Google) Exchange(ctx context.Context, code string) (*store.OAuthProfile, error) {
	tok, err := g.oauthCfg.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("google exchange: %w", err)
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, g.userInfoURL, nil)
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	resp, err := g.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("google userinfo: http %d", resp.StatusCode)
	}
	var u struct {
		ID      string `json:"id"`
		Email   string `json:"email"`
		Name    string `json:"name"`
		Picture string `json:"picture"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return nil, err
	}
	return &store.OAuthProfile{
		Provider:  "google",
		Subject:   u.ID,
		Email:     u.Email,
		Name:      u.Name,
		AvatarURL: u.Picture,
	}, nil
}
```

- [ ] **Step 4: Run tests + commit**

```bash
go test ./internal/oauth/...
git add internal/oauth
git commit -m "feat(oauth): Google provider"
```

---

## Task 12: Auth state store + handlers

**Goal:** `/auth/<provider>/login` issues a state cookie and 302s; `/auth/<provider>/callback` validates state, calls `provider.Exchange`, upserts user, creates auth_session, sets cookies.

**Files:**
- Create: `internal/api/auth_oauth.go`
- Create: `internal/api/auth_oauth_test.go`

- [ ] **Step 1: Write failing test**

`internal/api/auth_oauth_test.go`:

```go
package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jleal52/claude-switch/internal/oauth"
	"github.com/jleal52/claude-switch/internal/store"
	"github.com/stretchr/testify/require"
)

// fakeProvider returns a fixed profile on Exchange and a deterministic auth URL.
type fakeProvider struct{ name string; profile store.OAuthProfile }

func (f *fakeProvider) Name() string                                      { return f.name }
func (f *fakeProvider) AuthCodeURL(state string) string                    { return "https://oauth.test/auth?state=" + state }
func (f *fakeProvider) Exchange(ctx context.Context, code string) (*store.OAuthProfile, error) {
	cp := f.profile
	return &cp, nil
}

func TestLoginRedirectsAndSetsStateCookie(t *testing.T) {
	s := newTestStore(t, "auth_login")
	prov := &fakeProvider{name: "github", profile: store.OAuthProfile{Provider: "github", Subject: "1"}}
	h := NewAuthHandlers(AuthConfig{
		Store: s, Providers: []oauth.Provider{prov},
		BaseURL: "https://server.example.com", Secure: true,
	})

	req := httptest.NewRequest("GET", "/auth/github/login", nil)
	rr := httptest.NewRecorder()
	h.Login(rr, req)

	require.Equal(t, http.StatusFound, rr.Code)
	loc := rr.Header().Get("Location")
	require.True(t, strings.HasPrefix(loc, "https://oauth.test/auth?state="))

	cookies := rr.Result().Cookies()
	var stateCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "cs_oauth_state" {
			stateCookie = c
		}
	}
	require.NotNil(t, stateCookie)
	require.NotEmpty(t, stateCookie.Value)
}

func TestCallbackUpsertsUserAndIssuesSession(t *testing.T) {
	s := newTestStore(t, "auth_cb")
	prov := &fakeProvider{name: "github", profile: store.OAuthProfile{
		Provider: "github", Subject: "42", Email: "u@x", Name: "U",
	}}
	h := NewAuthHandlers(AuthConfig{
		Store: s, Providers: []oauth.Provider{prov},
		BaseURL: "https://server.example.com", Secure: true,
	})

	// First, login to capture the state.
	loginReq := httptest.NewRequest("GET", "/auth/github/login", nil)
	loginResp := httptest.NewRecorder()
	h.Login(loginResp, loginReq)
	state := getCookieValue(loginResp.Result().Cookies(), "cs_oauth_state")

	// Now callback with that state + a fake code.
	cbReq := httptest.NewRequest("GET", "/auth/github/callback?code=ok&state="+state, nil)
	cbReq.AddCookie(&http.Cookie{Name: "cs_oauth_state", Value: state})
	cbResp := httptest.NewRecorder()
	h.Callback(cbResp, cbReq)

	require.Equal(t, http.StatusFound, cbResp.Code)
	require.Equal(t, "/", cbResp.Header().Get("Location"))

	cs := getCookieValue(cbResp.Result().Cookies(), "cs_session")
	require.NotEmpty(t, cs)
	csrf := getCookieValue(cbResp.Result().Cookies(), "cs_csrf")
	require.NotEmpty(t, csrf)

	// User row created.
	users, _ := s.Users().UpsertOAuth(context.Background(), prov.profile)
	require.NotEmpty(t, users.ID)
}

func TestCallbackRejectsStateMismatch(t *testing.T) {
	s := newTestStore(t, "auth_cb_mismatch")
	prov := &fakeProvider{name: "github", profile: store.OAuthProfile{Provider: "github", Subject: "1"}}
	h := NewAuthHandlers(AuthConfig{Store: s, Providers: []oauth.Provider{prov}, BaseURL: "https://x", Secure: true})

	req := httptest.NewRequest("GET", "/auth/github/callback?code=ok&state=A", nil)
	req.AddCookie(&http.Cookie{Name: "cs_oauth_state", Value: "B"})
	rr := httptest.NewRecorder()
	h.Callback(rr, req)
	require.Equal(t, http.StatusBadRequest, rr.Code)
}

func getCookieValue(cookies []*http.Cookie, name string) string {
	for _, c := range cookies {
		if c.Name == name {
			return c.Value
		}
	}
	return ""
}

// newTestStore is duplicated locally so this test package can use it without
// importing internal/store/testhelpers (which has the t.Skip on no-docker).
// Implementation in this file delegates to that package via a small wrapper.
```

The `newTestStore` reference is to the helper in `internal/store/testhelpers.go`. Add this small wrapper in a new file `internal/api/testmain_test.go`:

```go
package api

import (
	"testing"

	"github.com/jleal52/claude-switch/internal/store"
)

// newTestStore wraps the store-package helper so handler tests can grab a
// fresh database without re-implementing the testcontainer plumbing.
func newTestStore(t *testing.T, label string) *store.Store {
	return store.NewTestStore(t, label)
}
```

And expose the helper from store by renaming the unexported one. Update `internal/store/testhelpers.go` so `newTestStore` is now exported as `NewTestStore`:

```go
// (rename, no other behavior change)
func NewTestStore(t *testing.T, label string) *Store { ... }
```

Update prior store tests (Tasks 3-7) that called `newTestStore(t, ...)` to call `NewTestStore(t, ...)` — global rename in the store package. Keep `MustStartMongo` as is.

- [ ] **Step 2: Run tests to confirm everything still green after rename**

```bash
go test ./internal/store/... -timeout 120s
```

Expected: all store tests still PASS with the renamed helper.

- [ ] **Step 3: Implement auth_oauth.go**

`internal/api/auth_oauth.go`:

```go
package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/jleal52/claude-switch/internal/csrf"
	"github.com/jleal52/claude-switch/internal/oauth"
	"github.com/jleal52/claude-switch/internal/store"
)

const (
	stateCookieName   = "cs_oauth_state"
	sessionCookieName = "cs_session"
	authSessionTTL    = 30 * 24 * time.Hour
	stateTTL          = 10 * time.Minute
)

type AuthConfig struct {
	Store     *store.Store
	Providers []oauth.Provider
	BaseURL   string // used for login redirect "after login"; we hardcode "/" for now
	Secure    bool   // sets Secure flag on cookies
}

type AuthHandlers struct {
	cfg       AuthConfig
	providers map[string]oauth.Provider
}

func NewAuthHandlers(cfg AuthConfig) *AuthHandlers {
	m := map[string]oauth.Provider{}
	for _, p := range cfg.Providers {
		m[p.Name()] = p
	}
	return &AuthHandlers{cfg: cfg, providers: m}
}

// Login handles GET /auth/{provider}/login.
func (h *AuthHandlers) Login(w http.ResponseWriter, r *http.Request) {
	name := pathSuffix(r.URL.Path, "/auth/", "/login")
	p, ok := h.providers[name]
	if !ok {
		http.NotFound(w, r)
		return
	}
	state, err := randomBase64URL(24)
	if err != nil {
		http.Error(w, "state", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookieName,
		Value:    state,
		Path:     "/",
		HttpOnly: true,
		Secure:   h.cfg.Secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(stateTTL.Seconds()),
	})
	http.Redirect(w, r, p.AuthCodeURL(state), http.StatusFound)
}

// Callback handles GET /auth/{provider}/callback?code=&state=.
func (h *AuthHandlers) Callback(w http.ResponseWriter, r *http.Request) {
	name := pathSuffix(r.URL.Path, "/auth/", "/callback")
	p, ok := h.providers[name]
	if !ok {
		http.NotFound(w, r)
		return
	}
	state := r.URL.Query().Get("state")
	c, err := r.Cookie(stateCookieName)
	if err != nil || c.Value == "" || c.Value != state {
		http.Error(w, "state mismatch", http.StatusBadRequest)
		return
	}
	// State has been used; clear the cookie immediately.
	http.SetCookie(w, &http.Cookie{Name: stateCookieName, Value: "", MaxAge: -1, Path: "/"})

	code := r.URL.Query().Get("code")
	prof, err := p.Exchange(r.Context(), code)
	if err != nil {
		http.Error(w, "oauth exchange", http.StatusBadGateway)
		return
	}

	user, err := h.cfg.Store.Users().UpsertOAuth(r.Context(), *prof)
	if err != nil {
		http.Error(w, "user upsert", http.StatusInternalServerError)
		return
	}
	sess, err := h.cfg.Store.AuthSessions().Create(r.Context(), user.ID, authSessionTTL)
	if err != nil {
		http.Error(w, "session", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    sess.ID,
		Path:     "/",
		HttpOnly: true,
		Secure:   h.cfg.Secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(authSessionTTL.Seconds()),
	})
	csrf.Set(w, sess.CSRFToken, h.cfg.Secure)

	http.Redirect(w, r, "/", http.StatusFound)
}

// Logout invalidates the auth session row and clears cookies.
func (h *AuthHandlers) Logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookieName); err == nil {
		_ = h.cfg.Store.AuthSessions().Delete(r.Context(), c.Value)
	}
	for _, n := range []string{sessionCookieName, csrf.CookieName} {
		http.SetCookie(w, &http.Cookie{Name: n, Value: "", Path: "/", MaxAge: -1})
	}
	w.WriteHeader(http.StatusNoContent)
}

// pathSuffix extracts middle of a path like /auth/X/login -> X. Returns "" if
// the path doesn't match the expected shape.
func pathSuffix(path, prefix, suffix string) string {
	if len(path) < len(prefix)+len(suffix)+1 || path[:len(prefix)] != prefix {
		return ""
	}
	rest := path[len(prefix):]
	if len(rest) < len(suffix) || rest[len(rest)-len(suffix):] != suffix {
		return ""
	}
	return rest[:len(rest)-len(suffix)]
}

// randomBase64URL returns a random URL-safe string of the given byte size.
func randomBase64URL(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := readRand(b); err != nil {
		return "", err
	}
	return base64URL(b), nil
}

// Indirection for tests; production reads crypto/rand.
var (
	readRand  = func(b []byte) (int, error) { return rand.Read(b) }
	base64URL = func(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
)

// Avoid unused-import noise for cases where rand/base64 aren't directly referenced.
var (
	_ = errors.New
	_ = context.Background
)
```

Add the imports: `crypto/rand` and `encoding/base64` at the top of the file.

- [ ] **Step 4: Run tests**

```bash
go test ./internal/api/... -timeout 60s
```

Expected: 3 auth tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/api internal/store
git commit -m "feat(api): OAuth login + callback + logout handlers"
```

---

## Task 13: Auth middleware + request context

**Goal:** Middleware that resolves the auth_session cookie to a `*store.User`, attaches it to request context, and rejects unauthenticated requests on protected routes. Also CSRF check on mutating methods.

**Files:**
- Create: `internal/api/middleware.go`
- Create: `internal/api/middleware_test.go`

- [ ] **Step 1: Write failing test**

`internal/api/middleware_test.go`:

```go
package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jleal52/claude-switch/internal/csrf"
	"github.com/stretchr/testify/require"
)

func TestAuthMiddlewareRejectsAnonymous(t *testing.T) {
	s := newTestStore(t, "mw_anon")
	mw := NewAuthMiddleware(s)
	called := false
	h := mw.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/api/me", nil))
	require.Equal(t, http.StatusUnauthorized, rr.Code)
	require.False(t, called)
}

func TestAuthMiddlewareInjectsUser(t *testing.T) {
	s := newTestStore(t, "mw_inject")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, fakeProfile("u1"))
	sess, _ := s.AuthSessions().Create(ctx, u.ID, time.Hour)

	mw := NewAuthMiddleware(s)
	var seen string
	h := mw.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ := UserFromContext(r.Context())
		seen = got.ID
	}))
	req := httptest.NewRequest("GET", "/api/me", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sess.ID})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)
	require.Equal(t, u.ID, seen)
}

func TestCSRFMiddlewareRejectsMutatingWithoutHeader(t *testing.T) {
	s := newTestStore(t, "mw_csrf")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, fakeProfile("u2"))
	sess, _ := s.AuthSessions().Create(ctx, u.ID, time.Hour)

	mw := NewAuthMiddleware(s)
	called := false
	h := mw.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))

	req := httptest.NewRequest("POST", "/api/foo", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sess.ID})
	req.AddCookie(&http.Cookie{Name: csrf.CookieName, Value: sess.CSRFToken})
	// no X-CSRF-Token header
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	require.Equal(t, http.StatusForbidden, rr.Code)
	require.False(t, called)
}

func fakeProfile(subject string) (p struct{}) { return }
```

The `fakeProfile` helper above returns the wrong type — replace with:

```go
import "github.com/jleal52/claude-switch/internal/store"

func fakeProfile(subject string) store.OAuthProfile {
	return store.OAuthProfile{Provider: "github", Subject: subject}
}
```

(merge with the existing import block; remove the wrong stub.)

Add `import "time"` if not already there.

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/api/... -run Middleware -timeout 60s
```

Expected: undefined `NewAuthMiddleware`, `UserFromContext`.

- [ ] **Step 3: Implement middleware.go**

`internal/api/middleware.go`:

```go
package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/jleal52/claude-switch/internal/csrf"
	"github.com/jleal52/claude-switch/internal/store"
)

type ctxKey int

const userCtxKey ctxKey = 0

type AuthMiddleware struct {
	store *store.Store
}

func NewAuthMiddleware(s *store.Store) *AuthMiddleware { return &AuthMiddleware{store: s} }

// Require returns an http.Handler that enforces an authenticated user. On
// state-changing methods (POST/PUT/PATCH/DELETE), it also enforces CSRF.
func (m *AuthMiddleware) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookieName)
		if err != nil || c.Value == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		sess, err := m.store.AuthSessions().GetByID(r.Context(), c.Value)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		// Refresh the auth session's expiry on every request (rolling 30 days).
		_ = m.store.AuthSessions().Touch(r.Context(), sess.ID, authSessionTTL)

		// CSRF for mutating methods.
		if isMutating(r.Method) {
			if err := csrf.Verify(r); err != nil {
				http.Error(w, "csrf: "+err.Error(), http.StatusForbidden)
				return
			}
			// Also ensure header value matches the user's session csrf_token,
			// not just any cookie/header pair.
			if r.Header.Get(csrf.HeaderName) != sess.CSRFToken {
				http.Error(w, "csrf: token mismatch", http.StatusForbidden)
				return
			}
		}

		user, err := m.store.Users().GetByID(r.Context(), sess.UserID)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), userCtxKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// UserFromContext returns the authenticated user attached by Require.
func UserFromContext(ctx context.Context) (*store.User, bool) {
	u, ok := ctx.Value(userCtxKey).(*store.User)
	return u, ok
}

// MustUser panics if no user is in context. Use only inside handlers wrapped
// by Require.
func MustUser(ctx context.Context) *store.User {
	u, ok := UserFromContext(ctx)
	if !ok {
		panic(errors.New("api: no user in context"))
	}
	return u
}

func isMutating(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}
```

- [ ] **Step 4: Run tests + commit**

```bash
go test ./internal/api/... -timeout 60s
git add internal/api
git commit -m "feat(api): auth middleware with rolling session + CSRF check"
```

---

## Task 14: /api/me + /api/auth/logout

**Goal:** Smallest authenticated endpoint — confirm the middleware works end-to-end.

**Files:**
- Create: `internal/api/me.go`
- Create: `internal/api/me_test.go`

- [ ] **Step 1: Write failing test**

`internal/api/me_test.go`:

```go
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jleal52/claude-switch/internal/csrf"
	"github.com/stretchr/testify/require"
)

func TestMeReturnsUser(t *testing.T) {
	s := newTestStore(t, "me_basic")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, fakeProfile("u1"))
	sess, _ := s.AuthSessions().Create(ctx, u.ID, time.Hour)

	h := NewMeHandlers(MeConfig{
		Store:               s,
		ProvidersConfigured: []string{"github"},
	})
	req := httptest.NewRequest("GET", "/api/me", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sess.ID})
	// CSRF NOT required for GET.
	rr := httptest.NewRecorder()
	NewAuthMiddleware(s).Require(http.HandlerFunc(h.Get)).ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	var got struct {
		User                struct{ ID string `json:"id"` } `json:"user"`
		ProvidersConfigured []string                        `json:"providers_configured"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
	require.Equal(t, u.ID, got.User.ID)
	require.Contains(t, got.ProvidersConfigured, "github")
}

func TestMePostSettingsRequiresCSRF(t *testing.T) {
	s := newTestStore(t, "me_settings")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, fakeProfile("u2"))
	sess, _ := s.AuthSessions().Create(ctx, u.ID, time.Hour)

	h := NewMeHandlers(MeConfig{Store: s, ProvidersConfigured: []string{"github"}})
	body := []byte(`{"keep_transcripts":true}`)
	req := httptest.NewRequest("POST", "/api/me/settings", bytesReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sess.ID})
	req.AddCookie(&http.Cookie{Name: csrf.CookieName, Value: sess.CSRFToken})
	req.Header.Set(csrf.HeaderName, sess.CSRFToken)

	rr := httptest.NewRecorder()
	NewAuthMiddleware(s).Require(http.HandlerFunc(h.UpdateSettings)).ServeHTTP(rr, req)
	require.Equal(t, http.StatusNoContent, rr.Code)

	got, _ := s.Users().GetByID(ctx, u.ID)
	require.True(t, got.KeepTranscripts)
}

// bytesReader is a tiny helper to build request bodies in tests.
func bytesReader(b []byte) *bytesReadCloser { return &bytesReadCloser{b: b} }

type bytesReadCloser struct{ b []byte }

func (r *bytesReadCloser) Read(p []byte) (int, error) {
	if len(r.b) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.b)
	r.b = r.b[n:]
	return n, nil
}
func (*bytesReadCloser) Close() error { return nil }
```

Add `import "io"` at the top of the test file.

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/api/... -run Me -timeout 60s
```

- [ ] **Step 3: Implement me.go**

`internal/api/me.go`:

```go
package api

import (
	"encoding/json"
	"net/http"

	"github.com/jleal52/claude-switch/internal/store"
)

type MeConfig struct {
	Store               *store.Store
	ProvidersConfigured []string // ["github"], ["google"], or both
}

type MeHandlers struct{ cfg MeConfig }

func NewMeHandlers(cfg MeConfig) *MeHandlers { return &MeHandlers{cfg: cfg} }

type meResponse struct {
	User struct {
		ID              string `json:"id"`
		Email           string `json:"email,omitempty"`
		Name            string `json:"name,omitempty"`
		AvatarURL       string `json:"avatar_url,omitempty"`
		KeepTranscripts bool   `json:"keep_transcripts"`
	} `json:"user"`
	ProvidersConfigured []string `json:"providers_configured"`
}

// Get is GET /api/me.
func (h *MeHandlers) Get(w http.ResponseWriter, r *http.Request) {
	u := MustUser(r.Context())
	resp := meResponse{ProvidersConfigured: h.cfg.ProvidersConfigured}
	resp.User.ID = u.ID
	resp.User.Email = u.Email
	resp.User.Name = u.Name
	resp.User.AvatarURL = u.AvatarURL
	resp.User.KeepTranscripts = u.KeepTranscripts
	writeJSON(w, http.StatusOK, resp)
}

// UpdateSettings is POST /api/me/settings.
func (h *MeHandlers) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		KeepTranscripts *bool `json:"keep_transcripts,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	u := MustUser(r.Context())
	if body.KeepTranscripts != nil {
		if err := h.cfg.Store.Users().SetKeepTranscripts(r.Context(), u.ID, *body.KeepTranscripts); err != nil {
			http.Error(w, "store", http.StatusInternalServerError)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeJSON marshals v as JSON and writes it with the given status code.
// Errors marshalling are panic-worthy (programmer error, not user input).
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
```

- [ ] **Step 4: Run + commit**

```bash
go test ./internal/api/... -timeout 60s
git add internal/api
git commit -m "feat(api): /api/me GET + /api/me/settings POST"
```

---

## Task 15: Wrappers handlers + device-code endpoints

**Goal:** `GET /api/wrappers`, `DELETE /api/wrappers/:id`, plus the wrapper-facing `/device/pair/start`, `/device/pair/poll`, `/device/token/refresh`.

**Files:**
- Create: `internal/api/wrappers.go`
- Create: `internal/api/device.go`
- Create: `internal/api/wrappers_test.go`
- Create: `internal/api/device_test.go`

- [ ] **Step 1: Write failing tests**

`internal/api/wrappers_test.go`:

```go
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jleal52/claude-switch/internal/csrf"
	"github.com/jleal52/claude-switch/internal/store"
	"github.com/stretchr/testify/require"
)

func TestWrappersListReturnsOnlyOwn(t *testing.T) {
	s := newTestStore(t, "wr_list")
	ctx := context.Background()
	u1, _ := s.Users().UpsertOAuth(ctx, fakeProfile("u1"))
	u2, _ := s.Users().UpsertOAuth(ctx, fakeProfile("u2"))
	_, _, _ = s.Wrappers().Create(ctx, store.WrapperCreate{UserID: u1.ID, Name: "mine", OS: "linux", Arch: "amd64"})
	_, _, _ = s.Wrappers().Create(ctx, store.WrapperCreate{UserID: u2.ID, Name: "other", OS: "linux", Arch: "amd64"})

	sess, _ := s.AuthSessions().Create(ctx, u1.ID, time.Hour)
	h := NewWrappersHandlers(s)
	req := httptest.NewRequest("GET", "/api/wrappers", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sess.ID})
	rr := httptest.NewRecorder()
	NewAuthMiddleware(s).Require(http.HandlerFunc(h.List)).ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	var got []struct {
		Name string `json:"name"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	require.Len(t, got, 1)
	require.Equal(t, "mine", got[0].Name)
}

func TestWrappersDeleteOnlyOwn(t *testing.T) {
	s := newTestStore(t, "wr_delete")
	ctx := context.Background()
	owner, _ := s.Users().UpsertOAuth(ctx, fakeProfile("o"))
	other, _ := s.Users().UpsertOAuth(ctx, fakeProfile("x"))
	w1, _, _ := s.Wrappers().Create(ctx, store.WrapperCreate{UserID: owner.ID, Name: "mine", OS: "linux", Arch: "amd64"})
	w2, _, _ := s.Wrappers().Create(ctx, store.WrapperCreate{UserID: other.ID, Name: "other", OS: "linux", Arch: "amd64"})

	sess, _ := s.AuthSessions().Create(ctx, owner.ID, time.Hour)
	h := NewWrappersHandlers(s)

	// Delete mine -> 204.
	req := httptest.NewRequest("DELETE", "/api/wrappers/"+w1.ID, nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sess.ID})
	req.AddCookie(&http.Cookie{Name: csrf.CookieName, Value: sess.CSRFToken})
	req.Header.Set(csrf.HeaderName, sess.CSRFToken)
	req.SetPathValue("id", w1.ID)
	rr := httptest.NewRecorder()
	NewAuthMiddleware(s).Require(http.HandlerFunc(h.Delete)).ServeHTTP(rr, req)
	require.Equal(t, http.StatusNoContent, rr.Code)

	// Delete other's -> 404 (not exposed).
	req = httptest.NewRequest("DELETE", "/api/wrappers/"+w2.ID, nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sess.ID})
	req.AddCookie(&http.Cookie{Name: csrf.CookieName, Value: sess.CSRFToken})
	req.Header.Set(csrf.HeaderName, sess.CSRFToken)
	req.SetPathValue("id", w2.ID)
	rr = httptest.NewRecorder()
	NewAuthMiddleware(s).Require(http.HandlerFunc(h.Delete)).ServeHTTP(rr, req)
	require.Equal(t, http.StatusNotFound, rr.Code)
}
```

`internal/api/device_test.go`:

```go
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDevicePairStartCreatesPendingCode(t *testing.T) {
	s := newTestStore(t, "dev_start")
	h := NewDeviceHandlers(s)

	body, _ := json.Marshal(map[string]string{
		"name": "ireland", "os": "linux", "arch": "amd64", "version": "0.1.0",
	})
	req := httptest.NewRequest("POST", "/device/pair/start", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.PairStart(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	var resp struct {
		Code      string `json:"code"`
		PollURL   string `json:"poll_url"`
		ExpiresIn int    `json:"expires_in"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	require.NotEmpty(t, resp.Code)
	require.Equal(t, "/device/pair/poll?c="+resp.Code, resp.PollURL)
}

func TestDevicePairPollPendingThenApproved(t *testing.T) {
	s := newTestStore(t, "dev_poll")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, fakeProfile("u1"))
	pc, _ := s.Pairing().Create(ctx, store.WrapperDescriptor{Name: "x", OS: "linux", Arch: "amd64"}, time.Minute)

	h := NewDeviceHandlers(s, WithServerEndpoint("wss://server.example.com/ws/wrapper"))

	// Pending -> 202.
	req := httptest.NewRequest("GET", "/device/pair/poll?c="+pc.Code, nil)
	rr := httptest.NewRecorder()
	h.PairPoll(rr, req)
	require.Equal(t, http.StatusAccepted, rr.Code)

	// Approve via store directly (would normally go through /api/pair/redeem).
	require.NoError(t, s.Pairing().Approve(ctx, pc.Code, u.ID))

	rr = httptest.NewRecorder()
	h.PairPoll(rr, httptest.NewRequest("GET", "/device/pair/poll?c="+pc.Code, nil))
	require.Equal(t, http.StatusOK, rr.Code)

	var creds struct {
		AccessToken    string `json:"access_token"`
		RefreshToken   string `json:"refresh_token"`
		ServerEndpoint string `json:"server_endpoint"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &creds)
	require.NotEmpty(t, creds.AccessToken)
	require.NotEmpty(t, creds.RefreshToken)
	require.Equal(t, "wss://server.example.com/ws/wrapper", creds.ServerEndpoint)

	// pairing_codes row deleted after redemption.
	_, err := s.Pairing().GetByCode(ctx, pc.Code)
	require.ErrorIs(t, err, store.ErrNotFound)
}

func TestDeviceTokenRefresh(t *testing.T) {
	s := newTestStore(t, "dev_refresh")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, fakeProfile("u1"))
	_, plain, _ := s.Wrappers().Create(ctx, store.WrapperCreate{UserID: u.ID, Name: "x", OS: "linux", Arch: "amd64"})

	h := NewDeviceHandlers(s, WithServerEndpoint("wss://server.example.com/ws/wrapper"))
	body, _ := json.Marshal(map[string]string{"refresh_token": plain})
	req := httptest.NewRequest("POST", "/device/token/refresh", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.TokenRefresh(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)
}

func TestDeviceTokenRefreshRevoked(t *testing.T) {
	s := newTestStore(t, "dev_refresh_rev")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, fakeProfile("u1"))
	w, plain, _ := s.Wrappers().Create(ctx, store.WrapperCreate{UserID: u.ID, Name: "x", OS: "linux", Arch: "amd64"})
	_ = s.Wrappers().Revoke(ctx, w.ID)

	h := NewDeviceHandlers(s, WithServerEndpoint("wss://server.example.com/ws/wrapper"))
	body, _ := json.Marshal(map[string]string{"refresh_token": plain})
	req := httptest.NewRequest("POST", "/device/token/refresh", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	h.TokenRefresh(rr, req)
	require.Equal(t, http.StatusUnauthorized, rr.Code)
}
```

Add `import "github.com/jleal52/claude-switch/internal/store"` to wrappers_test.go and device_test.go.

- [ ] **Step 2: Run tests to verify failures**

```bash
go test ./internal/api/... -run "Wrappers|Device" -timeout 60s
```

- [ ] **Step 3: Implement wrappers.go**

`internal/api/wrappers.go`:

```go
package api

import (
	"errors"
	"net/http"

	"github.com/jleal52/claude-switch/internal/store"
)

type WrappersHandlers struct{ store *store.Store }

func NewWrappersHandlers(s *store.Store) *WrappersHandlers { return &WrappersHandlers{store: s} }

type wrapperJSON struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	OS         string `json:"os"`
	Arch       string `json:"arch"`
	Version    string `json:"version"`
	PairedAt   string `json:"paired_at"`
	LastSeenAt string `json:"last_seen_at"`
	Revoked    bool   `json:"revoked"`
}

func (h *WrappersHandlers) List(w http.ResponseWriter, r *http.Request) {
	u := MustUser(r.Context())
	rows, err := h.store.Wrappers().ListByUser(r.Context(), u.ID)
	if err != nil {
		http.Error(w, "store", http.StatusInternalServerError)
		return
	}
	out := make([]wrapperJSON, 0, len(rows))
	for _, row := range rows {
		j := wrapperJSON{
			ID: row.ID, Name: row.Name, OS: row.OS, Arch: row.Arch, Version: row.Version,
			PairedAt: row.PairedAt.Format("2006-01-02T15:04:05Z07:00"),
			LastSeenAt: row.LastSeenAt.Format("2006-01-02T15:04:05Z07:00"),
			Revoked: row.RevokedAt != nil,
		}
		out = append(out, j)
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *WrappersHandlers) Delete(w http.ResponseWriter, r *http.Request) {
	u := MustUser(r.Context())
	id := r.PathValue("id")
	if id == "" {
		http.NotFound(w, r)
		return
	}
	rows, err := h.store.Wrappers().ListByUser(r.Context(), u.ID)
	if err != nil {
		http.Error(w, "store", http.StatusInternalServerError)
		return
	}
	owns := false
	for _, row := range rows {
		if row.ID == id {
			owns = true
			break
		}
	}
	if !owns {
		http.NotFound(w, r)
		return
	}
	if err := h.store.Wrappers().Revoke(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "store", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 4: Implement device.go**

`internal/api/device.go`:

```go
package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/jleal52/claude-switch/internal/store"
)

type DeviceHandlers struct {
	store          *store.Store
	serverEndpoint string
}

type DeviceOption func(*DeviceHandlers)

func WithServerEndpoint(url string) DeviceOption {
	return func(h *DeviceHandlers) { h.serverEndpoint = url }
}

func NewDeviceHandlers(s *store.Store, opts ...DeviceOption) *DeviceHandlers {
	h := &DeviceHandlers{store: s}
	for _, o := range opts {
		o(h)
	}
	return h
}

const pairTTL = 10 * time.Minute
const accessTokenTTL = 1 * time.Hour

func (h *DeviceHandlers) PairStart(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name    string `json:"name"`
		OS      string `json:"os"`
		Arch    string `json:"arch"`
		Version string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	pc, err := h.store.Pairing().Create(r.Context(), store.WrapperDescriptor{
		Name: in.Name, OS: in.OS, Arch: in.Arch, Version: in.Version,
	}, pairTTL)
	if err != nil {
		http.Error(w, "store", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"code":       pc.Code,
		"poll_url":   "/device/pair/poll?c=" + pc.Code,
		"expires_in": int(pairTTL.Seconds()),
	})
}

func (h *DeviceHandlers) PairPoll(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("c")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}
	pc, err := h.store.Pairing().GetByCode(r.Context(), code)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	switch pc.Status {
	case "pending":
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"status":"pending"}`))
		return
	case "denied":
		http.Error(w, `{"status":"denied"}`, http.StatusForbidden)
		_ = h.store.Pairing().Delete(r.Context(), code)
		return
	case "approved":
		// Create wrapper, issue tokens, delete pairing row.
		wRow, refresh, err := h.store.Wrappers().Create(r.Context(), store.WrapperCreate{
			UserID: pc.UserID, Name: pc.Wrapper.Name,
			OS: pc.Wrapper.OS, Arch: pc.Wrapper.Arch, Version: pc.Wrapper.Version,
		})
		if err != nil {
			http.Error(w, "store", http.StatusInternalServerError)
			return
		}
		access, expiresAt, err := h.store.WrapperTokens().Issue(r.Context(), wRow.ID, pc.UserID, accessTokenTTL)
		if err != nil {
			http.Error(w, "store", http.StatusInternalServerError)
			return
		}
		_ = h.store.Pairing().Delete(r.Context(), code)
		writeJSON(w, http.StatusOK, map[string]any{
			"access_token":    access,
			"refresh_token":   refresh,
			"expires_at":      expiresAt.Format(time.RFC3339),
			"server_endpoint": h.serverEndpoint,
		})
	default:
		http.Error(w, "unknown status", http.StatusInternalServerError)
	}
}

func (h *DeviceHandlers) TokenRefresh(w http.ResponseWriter, r *http.Request) {
	var in struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	wRow, err := h.store.Wrappers().VerifyRefreshToken(r.Context(), in.RefreshToken)
	if err != nil {
		if errors.Is(err, store.ErrRevoked) {
			http.Error(w, `{"error":"revoked"}`, http.StatusUnauthorized)
			return
		}
		http.Error(w, "invalid refresh token", http.StatusUnauthorized)
		return
	}
	access, expiresAt, err := h.store.WrapperTokens().Issue(r.Context(), wRow.ID, wRow.UserID, accessTokenTTL)
	if err != nil {
		http.Error(w, "store", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  access,
		"refresh_token": in.RefreshToken, // unchanged in MVP; rotate later
		"expires_at":    expiresAt.Format(time.RFC3339),
	})
}
```

- [ ] **Step 5: Run + commit**

```bash
go test ./internal/api/... -timeout 60s
git add internal/api
git commit -m "feat(api): /api/wrappers + device-code endpoints"
```

---

## Task 16: /api/pair/redeem

**Goal:** Authenticated browser endpoint to approve a pairing code.

**Files:**
- Create: `internal/api/pair.go`
- Create: `internal/api/pair_test.go`

- [ ] **Step 1: Write failing test**

`internal/api/pair_test.go`:

```go
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jleal52/claude-switch/internal/csrf"
	"github.com/jleal52/claude-switch/internal/store"
	"github.com/stretchr/testify/require"
)

func TestPairRedeemApproves(t *testing.T) {
	s := newTestStore(t, "pair_redeem")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, fakeProfile("u1"))
	pc, _ := s.Pairing().Create(ctx, store.WrapperDescriptor{Name: "x", OS: "linux", Arch: "amd64"}, time.Minute)
	sess, _ := s.AuthSessions().Create(ctx, u.ID, time.Hour)

	h := NewPairHandlers(s)
	body, _ := json.Marshal(map[string]string{"code": pc.Code})
	req := httptest.NewRequest("POST", "/api/pair/redeem", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sess.ID})
	req.AddCookie(&http.Cookie{Name: csrf.CookieName, Value: sess.CSRFToken})
	req.Header.Set(csrf.HeaderName, sess.CSRFToken)

	rr := httptest.NewRecorder()
	NewAuthMiddleware(s).Require(http.HandlerFunc(h.Redeem)).ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)

	got, _ := s.Pairing().GetByCode(ctx, pc.Code)
	require.Equal(t, "approved", got.Status)
	require.Equal(t, u.ID, got.UserID)
}

func TestPairRedeemUnknownCode(t *testing.T) {
	s := newTestStore(t, "pair_unknown")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, fakeProfile("u1"))
	sess, _ := s.AuthSessions().Create(ctx, u.ID, time.Hour)

	h := NewPairHandlers(s)
	body, _ := json.Marshal(map[string]string{"code": "ZZZZ-9999"})
	req := httptest.NewRequest("POST", "/api/pair/redeem", bytes.NewReader(body))
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sess.ID})
	req.AddCookie(&http.Cookie{Name: csrf.CookieName, Value: sess.CSRFToken})
	req.Header.Set(csrf.HeaderName, sess.CSRFToken)
	rr := httptest.NewRecorder()
	NewAuthMiddleware(s).Require(http.HandlerFunc(h.Redeem)).ServeHTTP(rr, req)
	require.Equal(t, http.StatusNotFound, rr.Code)
}
```

- [ ] **Step 2: Run + implement**

`internal/api/pair.go`:

```go
package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jleal52/claude-switch/internal/store"
)

type PairHandlers struct{ store *store.Store }

func NewPairHandlers(s *store.Store) *PairHandlers { return &PairHandlers{store: s} }

func (h *PairHandlers) Redeem(w http.ResponseWriter, r *http.Request) {
	u := MustUser(r.Context())
	var in struct {
		Code string `json:"code"`
		Deny bool   `json:"deny"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	pc, err := h.store.Pairing().GetByCode(r.Context(), in.Code)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if in.Deny {
		// Hard delete; the wrapper poll will then 404.
		_ = h.store.Pairing().Delete(r.Context(), pc.Code)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := h.store.Pairing().Approve(r.Context(), pc.Code, u.ID); err != nil {
		if errors.Is(err, store.ErrAlreadyApproved) {
			http.Error(w, "already approved", http.StatusConflict)
			return
		}
		http.Error(w, "store", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name":    pc.Wrapper.Name,
		"os":      pc.Wrapper.OS,
		"arch":    pc.Wrapper.Arch,
		"version": pc.Wrapper.Version,
	})
}
```

- [ ] **Step 3: Run + commit**

```bash
go test ./internal/api/... -timeout 60s
git add internal/api
git commit -m "feat(api): /api/pair/redeem endpoint"
```

---

## Task 17: Sessions handlers

**Goal:** `GET /api/sessions`, `POST /api/sessions`, `DELETE /api/sessions/:id`. The POST stores the session row but doesn't yet talk to the wrapper — that integration is Task 22+. For now, the row is created with status="starting" and an open_session command is enqueued via a hub interface that defaults to a stub.

**Files:**
- Create: `internal/api/sessions.go`
- Create: `internal/api/sessions_test.go`
- Create: `internal/hub/dispatcher.go` (interface only)

- [ ] **Step 1: Define the hub dispatcher interface**

`internal/hub/dispatcher.go`:

```go
// Package hub will hold the in-memory routing tables in Task 22. For now,
// only the Dispatcher interface lives here so the api package can depend
// on a stable contract.
package hub

import "context"

type OpenSessionRequest struct {
	WrapperID string
	SessionID string
	Cwd       string
	Account   string
	Args      []string
}

// Dispatcher is implemented by the hub and consumed by /api/sessions.
// Tests pass a fake implementation.
type Dispatcher interface {
	OpenSession(ctx context.Context, req OpenSessionRequest) error
	CloseSession(ctx context.Context, sessionID string) error
}
```

- [ ] **Step 2: Write failing test**

`internal/api/sessions_test.go`:

```go
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/jleal52/claude-switch/internal/csrf"
	"github.com/jleal52/claude-switch/internal/hub"
	"github.com/jleal52/claude-switch/internal/store"
	"github.com/stretchr/testify/require"
)

type fakeDispatcher struct {
	mu       sync.Mutex
	opens    []hub.OpenSessionRequest
	closes   []string
	openErr  error
	closeErr error
}

func (f *fakeDispatcher) OpenSession(ctx context.Context, req hub.OpenSessionRequest) error {
	f.mu.Lock(); defer f.mu.Unlock()
	f.opens = append(f.opens, req)
	return f.openErr
}
func (f *fakeDispatcher) CloseSession(ctx context.Context, id string) error {
	f.mu.Lock(); defer f.mu.Unlock()
	f.closes = append(f.closes, id)
	return f.closeErr
}

func TestSessionsCreateInsertsRowAndDispatches(t *testing.T) {
	s := newTestStore(t, "sess_create")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, fakeProfile("u1"))
	w, _, _ := s.Wrappers().Create(ctx, store.WrapperCreate{UserID: u.ID, Name: "x", OS: "linux", Arch: "amd64"})
	sess, _ := s.AuthSessions().Create(ctx, u.ID, time.Hour)

	d := &fakeDispatcher{}
	h := NewSessionsHandlers(s, d)
	body, _ := json.Marshal(map[string]any{"wrapper_id": w.ID, "cwd": "/tmp", "account": "default"})
	req := httptest.NewRequest("POST", "/api/sessions", bytes.NewReader(body))
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sess.ID})
	req.AddCookie(&http.Cookie{Name: csrf.CookieName, Value: sess.CSRFToken})
	req.Header.Set(csrf.HeaderName, sess.CSRFToken)
	rr := httptest.NewRecorder()
	NewAuthMiddleware(s).Require(http.HandlerFunc(h.Create)).ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	var got struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	require.NotEmpty(t, got.ID)
	require.Len(t, d.opens, 1)
	require.Equal(t, got.ID, d.opens[0].SessionID)
	require.Equal(t, w.ID, d.opens[0].WrapperID)
}

func TestSessionsCreateRejectsForeignWrapper(t *testing.T) {
	s := newTestStore(t, "sess_foreign")
	ctx := context.Background()
	owner, _ := s.Users().UpsertOAuth(ctx, fakeProfile("o"))
	other, _ := s.Users().UpsertOAuth(ctx, fakeProfile("x"))
	w, _, _ := s.Wrappers().Create(ctx, store.WrapperCreate{UserID: other.ID, Name: "x", OS: "linux", Arch: "amd64"})
	sess, _ := s.AuthSessions().Create(ctx, owner.ID, time.Hour)

	d := &fakeDispatcher{}
	h := NewSessionsHandlers(s, d)
	body, _ := json.Marshal(map[string]any{"wrapper_id": w.ID, "cwd": "/tmp", "account": "default"})
	req := httptest.NewRequest("POST", "/api/sessions", bytes.NewReader(body))
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sess.ID})
	req.AddCookie(&http.Cookie{Name: csrf.CookieName, Value: sess.CSRFToken})
	req.Header.Set(csrf.HeaderName, sess.CSRFToken)
	rr := httptest.NewRecorder()
	NewAuthMiddleware(s).Require(http.HandlerFunc(h.Create)).ServeHTTP(rr, req)

	require.Equal(t, http.StatusNotFound, rr.Code)
	require.Empty(t, d.opens)
}

func TestSessionsListReturnsOnlyOwn(t *testing.T) {
	s := newTestStore(t, "sess_list")
	ctx := context.Background()
	owner, _ := s.Users().UpsertOAuth(ctx, fakeProfile("o"))
	other, _ := s.Users().UpsertOAuth(ctx, fakeProfile("x"))
	w, _, _ := s.Wrappers().Create(ctx, store.WrapperCreate{UserID: owner.ID, Name: "x", OS: "linux", Arch: "amd64"})
	w2, _, _ := s.Wrappers().Create(ctx, store.WrapperCreate{UserID: other.ID, Name: "x", OS: "linux", Arch: "amd64"})

	_, _ = s.Sessions().Create(ctx, store.SessionCreate{ID: "s1", UserID: owner.ID, WrapperID: w.ID, Cwd: "/", Account: "default"})
	_, _ = s.Sessions().Create(ctx, store.SessionCreate{ID: "s2", UserID: other.ID, WrapperID: w2.ID, Cwd: "/", Account: "default"})

	sess, _ := s.AuthSessions().Create(ctx, owner.ID, time.Hour)
	d := &fakeDispatcher{}
	h := NewSessionsHandlers(s, d)
	req := httptest.NewRequest("GET", "/api/sessions", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sess.ID})
	rr := httptest.NewRecorder()
	NewAuthMiddleware(s).Require(http.HandlerFunc(h.List)).ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	var got []struct{ ID string `json:"id"` }
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	require.Len(t, got, 1)
	require.Equal(t, "s1", got[0].ID)
}

func TestSessionsDeleteOwnDispatches(t *testing.T) {
	s := newTestStore(t, "sess_del")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, fakeProfile("u"))
	w, _, _ := s.Wrappers().Create(ctx, store.WrapperCreate{UserID: u.ID, Name: "x", OS: "linux", Arch: "amd64"})
	_, _ = s.Sessions().Create(ctx, store.SessionCreate{ID: "ss", UserID: u.ID, WrapperID: w.ID, Cwd: "/", Account: "default"})
	sess, _ := s.AuthSessions().Create(ctx, u.ID, time.Hour)

	d := &fakeDispatcher{}
	h := NewSessionsHandlers(s, d)
	req := httptest.NewRequest("DELETE", "/api/sessions/ss", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sess.ID})
	req.AddCookie(&http.Cookie{Name: csrf.CookieName, Value: sess.CSRFToken})
	req.Header.Set(csrf.HeaderName, sess.CSRFToken)
	req.SetPathValue("id", "ss")
	rr := httptest.NewRecorder()
	NewAuthMiddleware(s).Require(http.HandlerFunc(h.Delete)).ServeHTTP(rr, req)

	require.Equal(t, http.StatusNoContent, rr.Code)
	require.Equal(t, []string{"ss"}, d.closes)
}
```

- [ ] **Step 3: Implement sessions.go**

`internal/api/sessions.go`:

```go
package api

import (
	"encoding/json"
	"net/http"

	"github.com/oklog/ulid/v2"

	"github.com/jleal52/claude-switch/internal/hub"
	"github.com/jleal52/claude-switch/internal/store"
)

type SessionsHandlers struct {
	store      *store.Store
	dispatcher hub.Dispatcher
}

func NewSessionsHandlers(s *store.Store, d hub.Dispatcher) *SessionsHandlers {
	return &SessionsHandlers{store: s, dispatcher: d}
}

type sessionJSON struct {
	ID         string  `json:"id"`
	WrapperID  string  `json:"wrapper_id"`
	JSONLUUID  string  `json:"jsonl_uuid,omitempty"`
	Cwd        string  `json:"cwd"`
	Account    string  `json:"account"`
	Status     string  `json:"status"`
	CreatedAt  string  `json:"created_at"`
	ExitedAt   string  `json:"exited_at,omitempty"`
	ExitCode   *int    `json:"exit_code,omitempty"`
	ExitReason string  `json:"exit_reason,omitempty"`
}

func (h *SessionsHandlers) List(w http.ResponseWriter, r *http.Request) {
	u := MustUser(r.Context())
	statusFilter := r.URL.Query().Get("status")
	rows, err := h.store.Sessions().ListByUser(r.Context(), u.ID, statusFilter)
	if err != nil {
		http.Error(w, "store", http.StatusInternalServerError)
		return
	}
	out := make([]sessionJSON, 0, len(rows))
	for _, row := range rows {
		j := sessionJSON{
			ID: row.ID, WrapperID: row.WrapperID, JSONLUUID: row.JSONLUUID,
			Cwd: row.Cwd, Account: row.Account, Status: row.Status,
			CreatedAt: row.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
			ExitCode: row.ExitCode, ExitReason: row.ExitReason,
		}
		if row.ExitedAt != nil {
			j.ExitedAt = row.ExitedAt.Format("2006-01-02T15:04:05Z07:00")
		}
		out = append(out, j)
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *SessionsHandlers) Create(w http.ResponseWriter, r *http.Request) {
	u := MustUser(r.Context())
	var in struct {
		WrapperID string   `json:"wrapper_id"`
		Cwd       string   `json:"cwd"`
		Account   string   `json:"account"`
		Args      []string `json:"args,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if in.Account == "" {
		in.Account = "default"
	}

	// Verify ownership of wrapper.
	wrappers, err := h.store.Wrappers().ListByUser(r.Context(), u.ID)
	if err != nil {
		http.Error(w, "store", http.StatusInternalServerError)
		return
	}
	owns := false
	for _, w := range wrappers {
		if w.ID == in.WrapperID && w.RevokedAt == nil {
			owns = true
			break
		}
	}
	if !owns {
		http.NotFound(w, r)
		return
	}

	sid := ulid.Make().String()
	row, err := h.store.Sessions().Create(r.Context(), store.SessionCreate{
		ID: sid, UserID: u.ID, WrapperID: in.WrapperID, Cwd: in.Cwd, Account: in.Account,
	})
	if err != nil {
		http.Error(w, "store", http.StatusInternalServerError)
		return
	}
	if err := h.dispatcher.OpenSession(r.Context(), hub.OpenSessionRequest{
		WrapperID: in.WrapperID, SessionID: sid,
		Cwd: in.Cwd, Account: in.Account, Args: in.Args,
	}); err != nil {
		// Mark session exited; the row stays for visibility.
		_ = h.store.Sessions().MarkExited(r.Context(), sid, -1, "spawn_failed", err.Error())
		http.Error(w, "dispatcher: "+err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, sessionJSON{
		ID: row.ID, WrapperID: row.WrapperID, Cwd: row.Cwd, Account: row.Account,
		Status: row.Status, CreatedAt: row.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	})
}

func (h *SessionsHandlers) Delete(w http.ResponseWriter, r *http.Request) {
	u := MustUser(r.Context())
	id := r.PathValue("id")
	if id == "" {
		http.NotFound(w, r)
		return
	}
	row, err := h.store.Sessions().GetByID(r.Context(), id)
	if err != nil || row.UserID != u.ID {
		http.NotFound(w, r)
		return
	}
	if err := h.dispatcher.CloseSession(r.Context(), id); err != nil {
		http.Error(w, "dispatcher", http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 4: Run + commit**

```bash
go test ./internal/api/... -timeout 60s
git add internal/api internal/hub
git commit -m "feat(api): /api/sessions list/create/delete + hub.Dispatcher contract"
```

---

## Task 18: /api/sessions/:id/messages

**Goal:** Stored transcript retrieval for opt-in users.

**Files:**
- Create: `internal/api/messages.go`
- Create: `internal/api/messages_test.go`

- [ ] **Step 1: Write failing test**

`internal/api/messages_test.go`:

```go
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jleal52/claude-switch/internal/store"
	"github.com/stretchr/testify/require"
)

func TestMessagesListReturnsForOwnSession(t *testing.T) {
	s := newTestStore(t, "msg_list")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, fakeProfile("u1"))
	w, _, _ := s.Wrappers().Create(ctx, store.WrapperCreate{UserID: u.ID, Name: "x", OS: "linux", Arch: "amd64"})
	_, _ = s.Sessions().Create(ctx, store.SessionCreate{ID: "s1", UserID: u.ID, WrapperID: w.ID, Cwd: "/", Account: "default"})

	t0 := time.Now().UTC()
	require.NoError(t, s.Messages().Append(ctx, "s1", u.ID, t0, `{"role":"user","content":"hi"}`))
	require.NoError(t, s.Messages().Append(ctx, "s1", u.ID, t0.Add(time.Second), `{"role":"assistant","content":"hello"}`))

	sess, _ := s.AuthSessions().Create(ctx, u.ID, time.Hour)
	h := NewMessagesHandlers(s)
	req := httptest.NewRequest("GET", "/api/sessions/s1/messages", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sess.ID})
	req.SetPathValue("id", "s1")
	rr := httptest.NewRecorder()
	NewAuthMiddleware(s).Require(http.HandlerFunc(h.List)).ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	var got []struct {
		Entry string `json:"entry"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	require.Len(t, got, 2)
}

func TestMessagesListForeignSessionIs404(t *testing.T) {
	s := newTestStore(t, "msg_foreign")
	ctx := context.Background()
	owner, _ := s.Users().UpsertOAuth(ctx, fakeProfile("o"))
	other, _ := s.Users().UpsertOAuth(ctx, fakeProfile("x"))
	w, _, _ := s.Wrappers().Create(ctx, store.WrapperCreate{UserID: other.ID, Name: "x", OS: "linux", Arch: "amd64"})
	_, _ = s.Sessions().Create(ctx, store.SessionCreate{ID: "s1", UserID: other.ID, WrapperID: w.ID, Cwd: "/", Account: "default"})

	sess, _ := s.AuthSessions().Create(ctx, owner.ID, time.Hour)
	h := NewMessagesHandlers(s)
	req := httptest.NewRequest("GET", "/api/sessions/s1/messages", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sess.ID})
	req.SetPathValue("id", "s1")
	rr := httptest.NewRecorder()
	NewAuthMiddleware(s).Require(http.HandlerFunc(h.List)).ServeHTTP(rr, req)
	require.Equal(t, http.StatusNotFound, rr.Code)
}
```

- [ ] **Step 2: Implement messages.go**

`internal/api/messages.go`:

```go
package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/jleal52/claude-switch/internal/store"
)

type MessagesHandlers struct{ store *store.Store }

func NewMessagesHandlers(s *store.Store) *MessagesHandlers { return &MessagesHandlers{store: s} }

type messageJSON struct {
	TS    string `json:"ts"`
	Entry string `json:"entry"`
}

const messagesPageMax = 1000

func (h *MessagesHandlers) List(w http.ResponseWriter, r *http.Request) {
	u := MustUser(r.Context())
	id := r.PathValue("id")
	row, err := h.store.Sessions().GetByID(r.Context(), id)
	if err != nil || row.UserID != u.ID {
		http.NotFound(w, r)
		return
	}

	var since time.Time
	if v := r.URL.Query().Get("since"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			http.Error(w, "bad since", http.StatusBadRequest)
			return
		}
		since = t
	}
	limit := messagesPageMax
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 || n > messagesPageMax {
			http.Error(w, "bad limit", http.StatusBadRequest)
			return
		}
		limit = n
	}

	rows, err := h.store.Messages().List(r.Context(), id, since, limit)
	if err != nil {
		http.Error(w, "store", http.StatusInternalServerError)
		return
	}
	out := make([]messageJSON, 0, len(rows))
	for _, m := range rows {
		out = append(out, messageJSON{
			TS:    m.TS.Format(time.RFC3339Nano),
			Entry: m.Entry,
		})
	}
	writeJSON(w, http.StatusOK, out)
}
```

- [ ] **Step 3: Run + commit**

```bash
go test ./internal/api/... -timeout 60s
git add internal/api
git commit -m "feat(api): /api/sessions/:id/messages with since+limit pagination"
```

---

## Task 19: Hub data structures + per-session ring snapshot

**Goal:** `Hub` owns `wrappers map[wrapperID]*WrapperConn` and `sessions map[sessionID]*SessionRoute` plus a per-session 32 KiB ring snapshot used for browser-side replay.

**Files:**
- Create: `internal/hub/hub.go`
- Create: `internal/hub/hub_test.go`

- [ ] **Step 1: Write failing test**

`internal/hub/hub_test.go`:

```go
package hub

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// fakeWrapperConn satisfies WrapperConn for tests.
type fakeWrapperConn struct {
	mu     sync.Mutex
	sent   []OutboundFrame
	closed bool
}

func (f *fakeWrapperConn) Send(fr OutboundFrame) error {
	f.mu.Lock(); defer f.mu.Unlock()
	if f.closed {
		return ErrWrapperOffline
	}
	f.sent = append(f.sent, fr)
	return nil
}
func (f *fakeWrapperConn) Close() {
	f.mu.Lock(); defer f.mu.Unlock()
	f.closed = true
}

func TestRegisterAndDispatchOpen(t *testing.T) {
	h := New()
	conn := &fakeWrapperConn{}
	h.RegisterWrapper("w1", conn)

	require.NoError(t, h.OpenSession(nil, OpenSessionRequest{
		WrapperID: "w1", SessionID: "s1", Cwd: "/", Account: "default",
	}))

	require.Len(t, conn.sent, 1)
	require.Equal(t, FrameTypeOpenSession, conn.sent[0].Type)
}

func TestOpenOnUnknownWrapperReturnsOffline(t *testing.T) {
	h := New()
	err := h.OpenSession(nil, OpenSessionRequest{
		WrapperID: "missing", SessionID: "s1", Cwd: "/", Account: "default",
	})
	require.ErrorIs(t, err, ErrWrapperOffline)
}

func TestRingCacheReplay(t *testing.T) {
	h := New()
	h.UpdateRing("s1", []byte("hello"))
	h.UpdateRing("s1", []byte(" world"))
	require.Equal(t, []byte("hello world"), h.SnapshotRing("s1"))
}
```

- [ ] **Step 2: Implement hub.go**

`internal/hub/hub.go`:

```go
package hub

import (
	"context"
	"errors"
	"sync"

	"github.com/jleal52/claude-switch/internal/ring"
)

const ringBytesPerSession = 32 * 1024

var ErrWrapperOffline = errors.New("hub: wrapper offline")

type FrameType int

const (
	FrameTypeOpenSession FrameType = iota
	FrameTypeCloseSession
	FrameTypePTYInput
	FrameTypePTYResize
	FrameTypePing
)

// OutboundFrame is what the hub asks a WrapperConn to send. The wswrapper
// package translates these into wire frames using internal/proto.
type OutboundFrame struct {
	Type      FrameType
	SessionID string
	JSON      any    // for open_session, close_session, pty.resize, ping
	Binary    []byte // for pty.input
}

// WrapperConn is implemented by the wswrapper package; the hub uses it as
// a write-only channel.
type WrapperConn interface {
	Send(OutboundFrame) error
	Close()
}

// BrowserConn is implemented by wsbrowser; hub fans pty.data out to all
// subscribers.
type BrowserConn interface {
	SendPTYData(b []byte) error
	SendControl(typ string, payload any) error
	Close(code int, reason string)
}

type Hub struct {
	mu          sync.RWMutex
	wrappers    map[string]WrapperConn
	sessionWrap map[string]string                  // sessionID -> wrapperID
	subscribers map[string]map[BrowserConn]struct{} // sessionID -> set of browser conns
	rings       map[string]*ring.Buffer
}

func New() *Hub {
	return &Hub{
		wrappers:    map[string]WrapperConn{},
		sessionWrap: map[string]string{},
		subscribers: map[string]map[BrowserConn]struct{}{},
		rings:       map[string]*ring.Buffer{},
	}
}

func (h *Hub) RegisterWrapper(id string, conn WrapperConn) {
	h.mu.Lock(); defer h.mu.Unlock()
	if old, ok := h.wrappers[id]; ok && old != conn {
		old.Close()
	}
	h.wrappers[id] = conn
}

func (h *Hub) UnregisterWrapper(id string) {
	h.mu.Lock()
	conn := h.wrappers[id]
	delete(h.wrappers, id)
	// Detach all sessions tied to this wrapper.
	var orphans []string
	for sid, wid := range h.sessionWrap {
		if wid == id {
			orphans = append(orphans, sid)
		}
	}
	subs := make(map[string]map[BrowserConn]struct{}, len(orphans))
	for _, sid := range orphans {
		delete(h.sessionWrap, sid)
		if set, ok := h.subscribers[sid]; ok {
			subs[sid] = set
			delete(h.subscribers, sid)
		}
	}
	h.mu.Unlock()
	if conn != nil {
		conn.Close()
	}
	for sid, set := range subs {
		_ = sid
		for b := range set {
			_ = b.SendControl("wrapper.offline", nil)
			b.Close(1011, "wrapper offline")
		}
	}
}

// OpenSession asks the wrapper to open a session and tracks the
// session->wrapper binding for later input/output routing.
func (h *Hub) OpenSession(ctx context.Context, req OpenSessionRequest) error {
	h.mu.Lock()
	conn, ok := h.wrappers[req.WrapperID]
	if !ok {
		h.mu.Unlock()
		return ErrWrapperOffline
	}
	h.sessionWrap[req.SessionID] = req.WrapperID
	h.mu.Unlock()
	return conn.Send(OutboundFrame{
		Type: FrameTypeOpenSession, SessionID: req.SessionID,
		JSON: map[string]any{
			"session": req.SessionID, "cwd": req.Cwd,
			"account": req.Account, "args": req.Args,
		},
	})
}

func (h *Hub) CloseSession(ctx context.Context, sessionID string) error {
	h.mu.Lock()
	wid, ok := h.sessionWrap[sessionID]
	if !ok {
		h.mu.Unlock()
		return nil // already closed/unknown — treat as success
	}
	conn := h.wrappers[wid]
	delete(h.sessionWrap, sessionID)
	delete(h.rings, sessionID)
	subs := h.subscribers[sessionID]
	delete(h.subscribers, sessionID)
	h.mu.Unlock()

	if conn != nil {
		_ = conn.Send(OutboundFrame{Type: FrameTypeCloseSession, SessionID: sessionID})
	}
	for b := range subs {
		b.Close(1000, "session closed")
	}
	return nil
}

// Subscribe registers a browser to receive pty.data + control frames for a
// session. Returns the current ring snapshot for replay (or nil).
func (h *Hub) Subscribe(sessionID string, b BrowserConn) ([]byte, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.sessionWrap[sessionID]; !ok {
		return nil, ErrWrapperOffline
	}
	set, ok := h.subscribers[sessionID]
	if !ok {
		set = map[BrowserConn]struct{}{}
		h.subscribers[sessionID] = set
	}
	set[b] = struct{}{}
	if rb, ok := h.rings[sessionID]; ok {
		return rb.Snapshot(), nil
	}
	return nil, nil
}

func (h *Hub) Unsubscribe(sessionID string, b BrowserConn) {
	h.mu.Lock(); defer h.mu.Unlock()
	if set, ok := h.subscribers[sessionID]; ok {
		delete(set, b)
		if len(set) == 0 {
			delete(h.subscribers, sessionID)
		}
	}
}

// FanoutPTYData sends bytes to all subscribers and updates the ring cache.
func (h *Hub) FanoutPTYData(sessionID string, payload []byte) {
	h.UpdateRing(sessionID, payload)
	h.mu.RLock()
	subs := make([]BrowserConn, 0, len(h.subscribers[sessionID]))
	for b := range h.subscribers[sessionID] {
		subs = append(subs, b)
	}
	h.mu.RUnlock()
	for _, b := range subs {
		_ = b.SendPTYData(payload)
	}
}

func (h *Hub) FanoutControl(sessionID, typ string, payload any) {
	h.mu.RLock()
	subs := make([]BrowserConn, 0, len(h.subscribers[sessionID]))
	for b := range h.subscribers[sessionID] {
		subs = append(subs, b)
	}
	h.mu.RUnlock()
	for _, b := range subs {
		_ = b.SendControl(typ, payload)
	}
}

// SendInput forwards browser keystrokes to the wrapper.
func (h *Hub) SendInput(sessionID string, payload []byte) error {
	h.mu.RLock()
	wid, ok := h.sessionWrap[sessionID]
	if !ok {
		h.mu.RUnlock()
		return ErrWrapperOffline
	}
	conn := h.wrappers[wid]
	h.mu.RUnlock()
	if conn == nil {
		return ErrWrapperOffline
	}
	return conn.Send(OutboundFrame{Type: FrameTypePTYInput, SessionID: sessionID, Binary: payload})
}

func (h *Hub) UpdateRing(sessionID string, payload []byte) {
	h.mu.Lock()
	rb, ok := h.rings[sessionID]
	if !ok {
		rb = ring.New(ringBytesPerSession)
		h.rings[sessionID] = rb
	}
	h.mu.Unlock()
	_, _ = rb.Write(payload)
}

func (h *Hub) SnapshotRing(sessionID string) []byte {
	h.mu.RLock()
	rb := h.rings[sessionID]
	h.mu.RUnlock()
	if rb == nil {
		return nil
	}
	return rb.Snapshot()
}

// AssignSession is called by the wswrapper when a wrapper's hello reports a
// session that's already alive (reconnect case).
func (h *Hub) AssignSession(sessionID, wrapperID string) {
	h.mu.Lock(); defer h.mu.Unlock()
	h.sessionWrap[sessionID] = wrapperID
}
```

- [ ] **Step 3: Run + commit**

```bash
go test ./internal/hub/... -timeout 30s
git add internal/hub
git commit -m "feat(hub): in-memory routing + per-session ring snapshot"
```

---

## Task 20: WS wrapper handler

**Goal:** `/ws/wrapper` upgrade. Validates `Authorization: Bearer <token>`, handles incoming frames, fans out to subscribers and updates DB. Reconciliation in hello.

**Files:**
- Create: `internal/wswrapper/wswrapper.go`
- Create: `internal/wswrapper/wswrapper_test.go`

- [ ] **Step 1: Write failing test**

`internal/wswrapper/wswrapper_test.go`:

```go
package wswrapper

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/require"

	"github.com/jleal52/claude-switch/internal/hub"
	"github.com/jleal52/claude-switch/internal/proto"
	"github.com/jleal52/claude-switch/internal/store"
)

func TestWrapperHelloRegistersAndReconciles(t *testing.T) {
	s := store.NewTestStore(t, "wsw_hello")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, store.OAuthProfile{Provider: "github", Subject: "u1"})
	wRow, _, _ := s.Wrappers().Create(ctx, store.WrapperCreate{UserID: u.ID, Name: "x", OS: "linux", Arch: "amd64"})
	access, _, _ := s.WrapperTokens().Issue(ctx, wRow.ID, u.ID, time.Hour)

	h := hub.New()
	srv := httptest.NewServer(NewHandler(s, h))
	defer srv.Close()
	wsURL := "ws" + srv.URL[len("http"):]

	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+access)
	conn, _, err := websocket.Dial(context.Background(), wsURL, &websocket.DialOptions{HTTPHeader: headers})
	require.NoError(t, err)
	defer conn.CloseNow()

	hello := proto.Hello{
		WrapperID: "x", OS: "linux", Arch: "amd64", Version: "0.1.0",
		Accounts: []string{"default"}, Capabilities: []string{"pty"},
	}
	raw, _ := proto.Encode(proto.TypeHello, "", hello)
	require.NoError(t, conn.Write(context.Background(), websocket.MessageText, raw))

	// Give the handler a moment to register.
	time.Sleep(50 * time.Millisecond)

	// Check last_seen_at was updated.
	got, _ := s.Wrappers().ListByUser(ctx, u.ID)
	require.Len(t, got, 1)
	require.True(t, got[0].LastSeenAt.After(wRow.LastSeenAt))
}

func TestWrapperRejectsBadToken(t *testing.T) {
	s := store.NewTestStore(t, "wsw_bad")
	h := hub.New()
	srv := httptest.NewServer(NewHandler(s, h))
	defer srv.Close()
	wsURL := "ws" + srv.URL[len("http"):]

	headers := http.Header{}
	headers.Set("Authorization", "Bearer not-a-token")
	_, resp, err := websocket.Dial(context.Background(), wsURL, &websocket.DialOptions{HTTPHeader: headers})
	require.Error(t, err)
	require.NotNil(t, resp)
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestWrapperRelaysSessionStartedToBrowsers(t *testing.T) {
	s := store.NewTestStore(t, "wsw_started")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, store.OAuthProfile{Provider: "github", Subject: "u1"})
	wRow, _, _ := s.Wrappers().Create(ctx, store.WrapperCreate{UserID: u.ID, Name: "x", OS: "linux", Arch: "amd64"})
	_, _ = s.Sessions().Create(ctx, store.SessionCreate{ID: "s1", UserID: u.ID, WrapperID: wRow.ID, Cwd: "/", Account: "default"})

	access, _, _ := s.WrapperTokens().Issue(ctx, wRow.ID, u.ID, time.Hour)
	h := hub.New()
	srv := httptest.NewServer(NewHandler(s, h))
	defer srv.Close()
	wsURL := "ws" + srv.URL[len("http"):]

	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+access)
	conn, _, err := websocket.Dial(context.Background(), wsURL, &websocket.DialOptions{HTTPHeader: headers})
	require.NoError(t, err)
	defer conn.CloseNow()

	// Hello first.
	hello := proto.Hello{
		WrapperID: "x", OS: "linux", Arch: "amd64", Version: "0.1.0",
		Accounts: []string{"default"}, Capabilities: []string{"pty"},
	}
	raw, _ := proto.Encode(proto.TypeHello, "", hello)
	_ = conn.Write(context.Background(), websocket.MessageText, raw)

	// Session started.
	ssRaw, _ := proto.Encode(proto.TypeSessionStarted, "s1", proto.SessionStarted{
		PID: 99, JSONLUUID: "u1", Cwd: "/", Account: "default",
	})
	_ = conn.Write(context.Background(), websocket.MessageText, ssRaw)

	// Wait for handler to update DB.
	require.Eventually(t, func() bool {
		got, err := s.Sessions().GetByID(ctx, "s1")
		return err == nil && got.Status == "running" && got.JSONLUUID == "u1"
	}, 2*time.Second, 20*time.Millisecond)

	// Cleanup.
	_ = json.RawMessage{}
}
```

- [ ] **Step 2: Implement wswrapper.go**

`internal/wswrapper/wswrapper.go`:

```go
// Package wswrapper handles the /ws/wrapper endpoint where wrappers
// connect. It owns the goroutines that read frames from a wrapper, fan them
// out to browser subscribers via the hub, and persist state changes to Mongo.
package wswrapper

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/oklog/ulid/v2"

	"github.com/jleal52/claude-switch/internal/hub"
	"github.com/jleal52/claude-switch/internal/proto"
	"github.com/jleal52/claude-switch/internal/store"
)

type Handler struct {
	store *store.Store
	hub   *hub.Hub
}

func NewHandler(s *store.Store, h *hub.Hub) http.Handler {
	return &Handler{store: s, hub: h}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	tok := bearerToken(r.Header.Get("Authorization"))
	if tok == "" {
		http.Error(w, "missing bearer", http.StatusUnauthorized)
		return
	}
	at, err := h.store.WrapperTokens().Verify(r.Context(), tok)
	if err != nil {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	c.SetReadLimit(8 * 1024 * 1024)

	wrapperID := at.WrapperID
	conn := newWrapperConn(c)
	h.hub.RegisterWrapper(wrapperID, conn)
	defer func() {
		h.hub.UnregisterWrapper(wrapperID)
		// Mark all live sessions of this wrapper as offline.
		_, _ = h.store.Sessions().MarkWrapperOffline(context.Background(), wrapperID)
		c.CloseNow()
	}()

	ctx := r.Context()
	for {
		typ, data, err := c.Read(ctx)
		if err != nil {
			return
		}
		switch typ {
		case websocket.MessageBinary:
			id, payload, err := proto.DecodePTYData(data)
			if err == nil {
				h.hub.FanoutPTYData(id.String(), payload)
			}
		case websocket.MessageText:
			h.handleText(ctx, wrapperID, at.UserID, data)
		}
	}
}

func (h *Handler) handleText(ctx context.Context, wrapperID, userID string, data []byte) {
	t, sessionID, payload, err := proto.Decode(data)
	if err != nil {
		return
	}
	switch t {
	case proto.TypeHello:
		var hello proto.Hello
		_ = json.Unmarshal([]byte(payload), &hello)
		_ = h.store.Wrappers().UpdateLastSeen(ctx, wrapperID)
		h.reconcile(ctx, wrapperID, hello.Sessions)
	case proto.TypeSessionStarted:
		var ss proto.SessionStarted
		_ = json.Unmarshal([]byte(payload), &ss)
		_ = h.store.Sessions().MarkRunning(ctx, sessionID, ss.JSONLUUID)
		h.hub.AssignSession(sessionID, wrapperID)
		h.hub.FanoutControl(sessionID, "session.started", ss)
	case proto.TypeSessionExited:
		var se proto.SessionExited
		_ = json.Unmarshal([]byte(payload), &se)
		_ = h.store.Sessions().MarkExited(ctx, sessionID, se.ExitCode, se.Reason, se.Detail)
		h.hub.FanoutControl(sessionID, "session.exited", se)
	case proto.TypeJSONLTail:
		var jt proto.JSONLTail
		_ = json.Unmarshal([]byte(payload), &jt)
		// Only persist if user opted in.
		if u, err := h.store.Users().GetByID(ctx, userID); err == nil && u.KeepTranscripts {
			_ = h.store.Messages().Append(ctx, sessionID, userID, time.Now(), jt.Entry)
		}
		h.hub.FanoutControl(sessionID, "jsonl.tail", jt)
	case proto.TypePong:
		// Liveness only.
	}
}

// reconcile compares the wrapper's live sessions with the server's catalog
// and updates statuses. Sessions in the DB as live for this wrapper but
// missing from the hello are marked exited (reason: wrapper_restart).
// Sessions in the hello that the server doesn't know are sent close_session.
func (h *Handler) reconcile(ctx context.Context, wrapperID string, helloSessions []proto.HelloSession) {
	live := map[string]bool{}
	for _, hs := range helloSessions {
		live[hs.ID] = true
		if err := h.store.Sessions().MarkRunningFromOffline(ctx, hs.ID); err == nil {
			h.hub.AssignSession(hs.ID, wrapperID)
		}
	}
	// Anything in DB still marked running/wrapper_offline for this wrapper but
	// not in `live` should be marked exited.
	rows, err := h.store.Sessions().ListByUser(ctx, "" /* not used */, "")
	if err == nil {
		for _, row := range rows {
			if row.WrapperID != wrapperID {
				continue
			}
			if row.Status == "running" || row.Status == "wrapper_offline" || row.Status == "starting" {
				if !live[row.ID] {
					_ = h.store.Sessions().MarkExited(ctx, row.ID, -1, "wrapper_restart", "")
				}
			}
		}
	}
}

func bearerToken(h string) string {
	const p = "Bearer "
	if !strings.HasPrefix(h, p) {
		return ""
	}
	return h[len(p):]
}

// wrapperConn implements hub.WrapperConn over a *websocket.Conn.
type wrapperConn struct {
	c *websocket.Conn
}

func newWrapperConn(c *websocket.Conn) *wrapperConn { return &wrapperConn{c: c} }

func (w *wrapperConn) Send(fr hub.OutboundFrame) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	switch fr.Type {
	case hub.FrameTypeOpenSession:
		raw, err := proto.Encode(proto.TypeOpenSession, fr.SessionID, fr.JSON)
		if err != nil {
			return err
		}
		return w.c.Write(ctx, websocket.MessageText, raw)
	case hub.FrameTypeCloseSession:
		raw, err := proto.Encode(proto.TypeCloseSession, fr.SessionID, struct{}{})
		if err != nil {
			return err
		}
		return w.c.Write(ctx, websocket.MessageText, raw)
	case hub.FrameTypePTYInput:
		id, err := ulid.ParseStrict(fr.SessionID)
		if err != nil {
			return err
		}
		raw := proto.EncodePTYData(id, fr.Binary)
		return w.c.Write(ctx, websocket.MessageBinary, raw)
	case hub.FrameTypePTYResize:
		raw, err := proto.Encode(proto.TypePTYResize, fr.SessionID, fr.JSON)
		if err != nil {
			return err
		}
		return w.c.Write(ctx, websocket.MessageText, raw)
	}
	return errors.New("wrapperConn: unknown frame type")
}

func (w *wrapperConn) Close() { _ = w.c.CloseNow() }

// Note: the reconcile() method above does a coarse "list all sessions" which
// is acceptable for MVP scale. When N grows, replace with a wrapper-scoped
// query. For now, ListByUser("","") returns all rows (filter cleanly bypassed
// because empty user_id matches no real user). The WrapperID filter inside
// the loop ensures correctness. Track this as tech debt.
```

- [ ] **Step 3: Tighten the reconcile query**

The above reconcile uses `ListByUser("", "")`. That's a bug — it'll return zero rows. Add a method to `internal/store/sessions.go`:

```go
// ListLiveByWrapper returns sessions for a wrapper in starting/running/
// wrapper_offline statuses. Used for hello reconciliation.
func (r *SessionsRepo) ListLiveByWrapper(ctx context.Context, wrapperID string) ([]Session, error) {
	cur, err := r.coll.Find(ctx, bson.M{
		"wrapper_id": objectIDFromHex(wrapperID),
		"status":     bson.M{"$in": []string{"starting", "running", "wrapper_offline"}},
	})
	if err != nil {
		return nil, err
	}
	var out []Session
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}
```

And update `reconcile` in wswrapper.go:

```go
rows, err := h.store.Sessions().ListLiveByWrapper(ctx, wrapperID)
if err == nil {
    for _, row := range rows {
        if !live[row.ID] {
            _ = h.store.Sessions().MarkExited(ctx, row.ID, -1, "wrapper_restart", "")
        }
    }
}
```

(Removing the `ListByUser` call entirely — that was a placeholder.)

- [ ] **Step 4: Run + commit**

```bash
go test ./internal/store/... ./internal/hub/... ./internal/wswrapper/... -timeout 120s
git add internal/store internal/wswrapper
git commit -m "feat(wswrapper): /ws/wrapper handler with hello reconciliation"
```

---

## Task 21: WS browser handler

**Goal:** `/ws/sessions/:id` upgrade. Validates cookie + CSRF query param, subscribes to the hub, sends replay, fans frames in both directions.

**Files:**
- Create: `internal/wsbrowser/wsbrowser.go`
- Create: `internal/wsbrowser/wsbrowser_test.go`

- [ ] **Step 1: Write failing test**

`internal/wsbrowser/wsbrowser_test.go`:

```go
package wsbrowser

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"

	"github.com/jleal52/claude-switch/internal/hub"
	"github.com/jleal52/claude-switch/internal/proto"
	"github.com/jleal52/claude-switch/internal/store"
)

// fakeWrapper registers itself with the hub and records what it's asked to send.
type fakeWrapper struct {
	frames []hub.OutboundFrame
}

func (f *fakeWrapper) Send(fr hub.OutboundFrame) error {
	f.frames = append(f.frames, fr)
	return nil
}
func (f *fakeWrapper) Close() {}

func TestBrowserSubscribeReceivesReplay(t *testing.T) {
	s := store.NewTestStore(t, "wsb_replay")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, store.OAuthProfile{Provider: "github", Subject: "u1"})
	wRow, _, _ := s.Wrappers().Create(ctx, store.WrapperCreate{UserID: u.ID, Name: "x", OS: "linux", Arch: "amd64"})
	sid := ulid.Make().String()
	_, _ = s.Sessions().Create(ctx, store.SessionCreate{ID: sid, UserID: u.ID, WrapperID: wRow.ID, Cwd: "/", Account: "default"})
	sess, _ := s.AuthSessions().Create(ctx, u.ID, time.Hour)

	h := hub.New()
	fw := &fakeWrapper{}
	h.RegisterWrapper(wRow.ID, fw)
	h.AssignSession(sid, wRow.ID)
	h.UpdateRing(sid, []byte("replayed-bytes"))

	mux := http.NewServeMux()
	mux.Handle("/ws/sessions/{id}", NewHandler(s, h))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	wsURL := "ws" + srv.URL[len("http"):] + "/ws/sessions/" + sid + "?ct=" + sess.CSRFToken
	headers := http.Header{}
	headers.Set("Cookie", "cs_session="+sess.ID)
	conn, _, err := websocket.Dial(context.Background(), wsURL, &websocket.DialOptions{HTTPHeader: headers})
	require.NoError(t, err)
	defer conn.CloseNow()

	// First text frame should be replay.start, then a binary replay frame, then replay.end.
	mtyp, raw, err := conn.Read(context.Background())
	require.NoError(t, err)
	require.Equal(t, websocket.MessageText, mtyp)
	tt, _, _, _ := proto.Decode(raw)
	require.Equal(t, "replay.start", tt)

	mtyp, raw, err = conn.Read(context.Background())
	require.NoError(t, err)
	require.Equal(t, websocket.MessageBinary, mtyp)

	mtyp, raw, err = conn.Read(context.Background())
	require.NoError(t, err)
	require.Equal(t, websocket.MessageText, mtyp)
	tt, _, _, _ = proto.Decode(raw)
	require.Equal(t, "replay.end", tt)

	_ = json.RawMessage{}
}

func TestBrowserRejectsBadCSRF(t *testing.T) {
	s := store.NewTestStore(t, "wsb_csrf")
	ctx := context.Background()
	u, _ := s.Users().UpsertOAuth(ctx, store.OAuthProfile{Provider: "github", Subject: "u1"})
	wRow, _, _ := s.Wrappers().Create(ctx, store.WrapperCreate{UserID: u.ID, Name: "x", OS: "linux", Arch: "amd64"})
	sid := "s1"
	_, _ = s.Sessions().Create(ctx, store.SessionCreate{ID: sid, UserID: u.ID, WrapperID: wRow.ID, Cwd: "/", Account: "default"})
	sess, _ := s.AuthSessions().Create(ctx, u.ID, time.Hour)

	mux := http.NewServeMux()
	mux.Handle("/ws/sessions/{id}", NewHandler(s, hub.New()))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	wsURL := "ws" + srv.URL[len("http"):] + "/ws/sessions/" + sid + "?ct=BAD"
	headers := http.Header{}
	headers.Set("Cookie", "cs_session="+sess.ID)
	_, resp, err := websocket.Dial(context.Background(), wsURL, &websocket.DialOptions{HTTPHeader: headers})
	require.Error(t, err)
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
}
```

- [ ] **Step 2: Implement wsbrowser.go**

`internal/wsbrowser/wsbrowser.go`:

```go
package wsbrowser

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/oklog/ulid/v2"

	"github.com/jleal52/claude-switch/internal/hub"
	"github.com/jleal52/claude-switch/internal/proto"
	"github.com/jleal52/claude-switch/internal/store"
)

type Handler struct {
	store *store.Store
	hub   *hub.Hub
}

func NewHandler(s *store.Store, h *hub.Hub) http.Handler {
	return &Handler{store: s, hub: h}
}

const (
	sessionCookieName = "cs_session"
)

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	auth, err := h.store.AuthSessions().GetByID(r.Context(), cookie.Value)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	csrfToken := r.URL.Query().Get("ct")
	if csrfToken == "" || csrfToken != auth.CSRFToken {
		http.Error(w, "csrf", http.StatusForbidden)
		return
	}

	sid := r.PathValue("id")
	row, err := h.store.Sessions().GetByID(r.Context(), sid)
	if err != nil || row.UserID != auth.UserID {
		http.NotFound(w, r)
		return
	}
	sessULID, err := ulid.ParseStrict(sid)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	c.SetReadLimit(4 * 1024 * 1024)
	bc := newBrowserConn(c, sessULID)

	replay, subErr := h.hub.Subscribe(sid, bc)
	if subErr != nil {
		_ = bc.SendControl("wrapper.offline", nil)
		bc.Close(1011, "wrapper offline")
		return
	}
	defer h.hub.Unsubscribe(sid, bc)

	// Send replay.start, optional binary replay, replay.end.
	_ = bc.SendControl("replay.start", map[string]string{"session": sid})
	if len(replay) > 0 {
		_ = bc.SendPTYData(replay)
	}
	_ = bc.SendControl("replay.end", map[string]string{"session": sid})

	// Read loop: forward keystrokes to the wrapper.
	ctx := r.Context()
	for {
		typ, data, err := c.Read(ctx)
		if err != nil {
			return
		}
		switch typ {
		case websocket.MessageBinary:
			_, payload, err := proto.DecodePTYData(data)
			if err == nil {
				_ = h.hub.SendInput(sid, payload)
			}
		case websocket.MessageText:
			// Only pty.resize handled here in MVP; ignore others.
		}
	}
}

// browserConn implements hub.BrowserConn.
type browserConn struct {
	c    *websocket.Conn
	mu   sync.Mutex
	id   ulid.ULID // session ULID for binary frames
}

func newBrowserConn(c *websocket.Conn, id ulid.ULID) *browserConn {
	return &browserConn{c: c, id: id}
}

func (b *browserConn) SendPTYData(payload []byte) error {
	b.mu.Lock(); defer b.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	frame := proto.EncodePTYData(b.id, payload)
	return b.c.Write(ctx, websocket.MessageBinary, frame)
}

func (b *browserConn) SendControl(typ string, payload any) error {
	b.mu.Lock(); defer b.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	raw, err := proto.Encode(typ, "", payload)
	if err != nil {
		return err
	}
	return b.c.Write(ctx, websocket.MessageText, raw)
}

func (b *browserConn) Close(code int, reason string) {
	b.mu.Lock(); defer b.mu.Unlock()
	_ = b.c.Close(websocket.StatusCode(code), reason)
}
```

- [ ] **Step 3: Run + commit**

```bash
go test ./internal/wsbrowser/... -timeout 60s
git add internal/wsbrowser
git commit -m "feat(wsbrowser): /ws/sessions/:id with replay + fan-out"
```

---

## Task 22: Router + main.go wiring

**Goal:** Wire everything together. `cmd/claude-switch-server/main.go` reads env, builds Store + Hub + Providers + Handlers, registers routes, listens on `:8080`.

**Files:**
- Create: `internal/api/router.go`
- Modify: `cmd/claude-switch-server/main.go`

- [ ] **Step 1: Implement router.go**

`internal/api/router.go`:

```go
package api

import (
	"net/http"

	"github.com/jleal52/claude-switch/internal/hub"
	"github.com/jleal52/claude-switch/internal/oauth"
	"github.com/jleal52/claude-switch/internal/store"
	"github.com/jleal52/claude-switch/internal/webfs"
	"github.com/jleal52/claude-switch/internal/wsbrowser"
	"github.com/jleal52/claude-switch/internal/wswrapper"
)

type RouterConfig struct {
	Store          *store.Store
	Hub            *hub.Hub
	Providers      []oauth.Provider
	BaseURL        string
	Secure         bool
	ServerEndpoint string // wss URL the wrapper should connect to
}

func NewRouter(cfg RouterConfig) http.Handler {
	mux := http.NewServeMux()

	// Health.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	// OAuth handlers.
	auth := NewAuthHandlers(AuthConfig{
		Store: cfg.Store, Providers: cfg.Providers,
		BaseURL: cfg.BaseURL, Secure: cfg.Secure,
	})
	mux.HandleFunc("GET /auth/{provider}/login", auth.Login)
	mux.HandleFunc("GET /auth/{provider}/callback", auth.Callback)

	// Device-code endpoints (anonymous, wrapper-facing).
	device := NewDeviceHandlers(cfg.Store, WithServerEndpoint(cfg.ServerEndpoint))
	mux.HandleFunc("POST /device/pair/start", device.PairStart)
	mux.HandleFunc("GET /device/pair/poll", device.PairPoll)
	mux.HandleFunc("POST /device/token/refresh", device.TokenRefresh)

	// Authenticated API.
	mw := NewAuthMiddleware(cfg.Store)
	me := NewMeHandlers(MeConfig{Store: cfg.Store, ProvidersConfigured: providerNames(cfg.Providers)})
	wrappers := NewWrappersHandlers(cfg.Store)
	pair := NewPairHandlers(cfg.Store)
	sessions := NewSessionsHandlers(cfg.Store, cfg.Hub)
	messages := NewMessagesHandlers(cfg.Store)

	mux.Handle("GET /api/me", mw.Require(http.HandlerFunc(me.Get)))
	mux.Handle("POST /api/me/settings", mw.Require(http.HandlerFunc(me.UpdateSettings)))
	mux.Handle("POST /api/auth/logout", mw.Require(http.HandlerFunc(auth.Logout)))
	mux.Handle("GET /api/wrappers", mw.Require(http.HandlerFunc(wrappers.List)))
	mux.Handle("DELETE /api/wrappers/{id}", mw.Require(http.HandlerFunc(wrappers.Delete)))
	mux.Handle("POST /api/pair/redeem", mw.Require(http.HandlerFunc(pair.Redeem)))
	mux.Handle("GET /api/sessions", mw.Require(http.HandlerFunc(sessions.List)))
	mux.Handle("POST /api/sessions", mw.Require(http.HandlerFunc(sessions.Create)))
	mux.Handle("DELETE /api/sessions/{id}", mw.Require(http.HandlerFunc(sessions.Delete)))
	mux.Handle("GET /api/sessions/{id}/messages", mw.Require(http.HandlerFunc(messages.List)))

	// WebSockets.
	mux.Handle("/ws/wrapper", wswrapper.NewHandler(cfg.Store, cfg.Hub))
	mux.Handle("/ws/sessions/{id}", wsbrowser.NewHandler(cfg.Store, cfg.Hub))

	// SPA fallback (handled inside webfs.Handler).
	mux.Handle("/", webfs.Handler())

	return mux
}

func providerNames(ps []oauth.Provider) []string {
	names := make([]string, 0, len(ps))
	for _, p := range ps {
		names = append(names, p.Name())
	}
	return names
}
```

- [ ] **Step 2: Replace main.go**

`cmd/claude-switch-server/main.go`:

```go
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jleal52/claude-switch/internal/api"
	"github.com/jleal52/claude-switch/internal/hub"
	"github.com/jleal52/claude-switch/internal/oauth"
	"github.com/jleal52/claude-switch/internal/store"
)

const serverVersion = "0.1.0-dev"

func main() {
	if err := run(); err != nil {
		slog.Error("server exited", "err", err)
		os.Exit(1)
	}
}

func run() error {
	flag.Parse()

	mongoURI := getenv("MONGO_URI", "mongodb://localhost:27017")
	mongoDB := getenv("MONGO_DB", "claude_switch")
	baseURL := mustenv("SERVER_BASE_URL")
	bindAddr := getenv("BIND_ADDR", ":8080")
	logLevel := getenv("LOG_LEVEL", "info")
	if os.Getenv("SESSION_SECRET") == "" {
		return fmt.Errorf("SESSION_SECRET is required")
	}

	configureLogger(logLevel)
	slog.Info("claude-switch-server starting", "version", serverVersion, "bind", bindAddr)

	ctx, cancel := signalContext()
	defer cancel()

	s, err := store.New(ctx, mongoURI, mongoDB)
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	defer s.Close(context.Background())

	h := hub.New()
	providers := buildProviders(baseURL)
	if len(providers) == 0 {
		return fmt.Errorf("no OAuth providers configured (set OAUTH_GITHUB_* or OAUTH_GOOGLE_*)")
	}

	wsServerEndpoint := strings.Replace(baseURL, "https://", "wss://", 1)
	wsServerEndpoint = strings.Replace(wsServerEndpoint, "http://", "ws://", 1) + "/ws/wrapper"

	router := api.NewRouter(api.RouterConfig{
		Store: s, Hub: h, Providers: providers,
		BaseURL: baseURL, Secure: strings.HasPrefix(baseURL, "https://"),
		ServerEndpoint: wsServerEndpoint,
	})

	srv := &http.Server{
		Addr: bindAddr, Handler: router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errc := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errc <- err
		}
	}()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		shutdownCtx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		return srv.Shutdown(shutdownCtx)
	}
}

func buildProviders(baseURL string) []oauth.Provider {
	var out []oauth.Provider
	if id, secret := os.Getenv("OAUTH_GITHUB_CLIENT_ID"), os.Getenv("OAUTH_GITHUB_CLIENT_SECRET"); id != "" && secret != "" {
		out = append(out, oauth.NewGitHub(oauth.GitHubConfig{
			ClientID: id, ClientSecret: secret,
			RedirectURL: baseURL + "/auth/github/callback",
		}))
	}
	if id, secret := os.Getenv("OAUTH_GOOGLE_CLIENT_ID"), os.Getenv("OAUTH_GOOGLE_CLIENT_SECRET"); id != "" && secret != "" {
		out = append(out, oauth.NewGoogle(oauth.GoogleConfig{
			ClientID: id, ClientSecret: secret,
			RedirectURL: baseURL + "/auth/google/callback",
		}))
	}
	return out
}

func configureLogger(level string) {
	lvl := slog.LevelInfo
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: lvl})))
}

func signalContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	go func() { <-ch; cancel() }()
	return ctx, cancel
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func mustenv(k string) string {
	v := os.Getenv(k)
	if v == "" {
		fmt.Fprintln(os.Stderr, k+" is required")
		os.Exit(2)
	}
	return v
}
```

- [ ] **Step 3: Build + commit**

```bash
go build ./...
git add cmd/claude-switch-server internal/api
git commit -m "feat(server): main.go wires store, hub, providers, router"
```

---

## Task 23: End-to-end integration test

**Goal:** Spin up the full server with a real Mongo (testcontainers) and a fake wrapper. Drive: pair → redeem → wrapper connects → browser creates session → browser subscribes → input → output round-trip.

**Files:**
- Create: `cmd/claude-switch-server/e2e_test.go`

- [ ] **Step 1: Write the test**

`cmd/claude-switch-server/e2e_test.go`:

```go
//go:build e2e

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/jleal52/claude-switch/internal/api"
	"github.com/jleal52/claude-switch/internal/hub"
	"github.com/jleal52/claude-switch/internal/oauth"
	"github.com/jleal52/claude-switch/internal/proto"
	"github.com/jleal52/claude-switch/internal/store"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"
)

// Stub OAuth provider so the test doesn't need real GitHub/Google.
type stubProvider struct{ name, email string }

func (s *stubProvider) Name() string                            { return s.name }
func (s *stubProvider) AuthCodeURL(state string) string         { return "/__stub_oauth?state=" + state }
func (s *stubProvider) Exchange(_ context.Context, _ string) (*store.OAuthProfile, error) {
	return &store.OAuthProfile{Provider: s.name, Subject: "stub-1", Email: s.email, Name: "Stub User"}, nil
}

func TestEndToEndPairOpenStream(t *testing.T) {
	st := store.NewTestStore(t, "e2e_full")
	h := hub.New()
	router := api.NewRouter(api.RouterConfig{
		Store: st, Hub: h,
		Providers: []oauth.Provider{&stubProvider{name: "github", email: "u@x"}},
		BaseURL: "http://localhost", Secure: false,
		ServerEndpoint: "", // filled in below
	})
	srv := httptest.NewServer(router)
	defer srv.Close()

	// 1. "Browser" logs in via stub callback to acquire session cookie.
	loginURL := srv.URL + "/auth/github/login"
	jar := newJar()
	hc := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := hc.Get(loginURL)
	require.NoError(t, err)
	resp.Body.Close()
	state := stateCookie(resp)

	cbURL := srv.URL + "/auth/github/callback?state=" + state + "&code=ok"
	resp2, err := hc.Get(cbURL)
	require.NoError(t, err)
	resp2.Body.Close()

	// 2. Wrapper pairs.
	body, _ := json.Marshal(map[string]string{"name": "ireland", "os": "linux", "arch": "amd64", "version": "0.1"})
	resp3, err := http.Post(srv.URL+"/device/pair/start", "application/json", strings.NewReader(string(body)))
	require.NoError(t, err)
	var pairing struct{ Code string }
	_ = json.NewDecoder(resp3.Body).Decode(&pairing)
	resp3.Body.Close()

	// 3. Browser redeems.
	redeemBody, _ := json.Marshal(map[string]string{"code": pairing.Code})
	req, _ := http.NewRequest("POST", srv.URL+"/api/pair/redeem", strings.NewReader(string(redeemBody)))
	req.Header.Set("Content-Type", "application/json")
	csrf := csrfFromJar(jar)
	req.Header.Set("X-CSRF-Token", csrf)
	resp4, err := hc.Do(req)
	require.NoError(t, err)
	resp4.Body.Close()

	// 4. Wrapper polls and gets credentials.
	resp5, err := http.Get(srv.URL + "/device/pair/poll?c=" + pairing.Code)
	require.NoError(t, err)
	var creds struct{ AccessToken, RefreshToken string }
	_ = json.NewDecoder(resp5.Body).Decode(&creds)
	resp5.Body.Close()
	require.NotEmpty(t, creds.AccessToken)

	// 5. Wrapper connects WS, says hello.
	wsURL := "ws" + srv.URL[len("http"):] + "/ws/wrapper"
	wHeaders := http.Header{}
	wHeaders.Set("Authorization", "Bearer "+creds.AccessToken)
	wConn, _, err := websocket.Dial(context.Background(), wsURL, &websocket.DialOptions{HTTPHeader: wHeaders})
	require.NoError(t, err)
	defer wConn.CloseNow()
	hello := proto.Hello{
		WrapperID: "ireland", OS: "linux", Arch: "amd64", Version: "0.1",
		Accounts: []string{"default"}, Capabilities: []string{"pty"},
	}
	helloRaw, _ := proto.Encode(proto.TypeHello, "", hello)
	_ = wConn.Write(context.Background(), websocket.MessageText, helloRaw)

	// 6. Browser creates session.
	wrappersResp, _ := hc.Get(srv.URL + "/api/wrappers")
	defer wrappersResp.Body.Close()
	var ws []struct{ ID string }
	_ = json.NewDecoder(wrappersResp.Body).Decode(&ws)
	require.Len(t, ws, 1)

	createBody, _ := json.Marshal(map[string]string{"wrapper_id": ws[0].ID, "cwd": "/tmp", "account": "default"})
	createReq, _ := http.NewRequest("POST", srv.URL+"/api/sessions", strings.NewReader(string(createBody)))
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("X-CSRF-Token", csrf)
	createResp, err := hc.Do(createReq)
	require.NoError(t, err)
	var sessJSON struct{ ID string }
	_ = json.NewDecoder(createResp.Body).Decode(&sessJSON)
	createResp.Body.Close()
	require.NotEmpty(t, sessJSON.ID)

	// 7. Wrapper acks the open with session.started.
	ssRaw, _ := proto.Encode(proto.TypeSessionStarted, sessJSON.ID, proto.SessionStarted{
		PID: 1, JSONLUUID: "uuid-x", Cwd: "/tmp", Account: "default",
	})
	_ = wConn.Write(context.Background(), websocket.MessageText, ssRaw)

	// 8. Browser subscribes; expects replay.start, replay.end.
	browserWS := "ws" + srv.URL[len("http"):] + "/ws/sessions/" + sessJSON.ID + "?ct=" + csrf
	bHeaders := http.Header{}
	bHeaders.Set("Cookie", sessionCookieFromJar(jar))
	bConn, _, err := websocket.Dial(context.Background(), browserWS, &websocket.DialOptions{HTTPHeader: bHeaders})
	require.NoError(t, err)
	defer bConn.CloseNow()

	mtyp, raw, _ := bConn.Read(context.Background())
	require.Equal(t, websocket.MessageText, mtyp)
	tt, _, _, _ := proto.Decode(raw)
	require.Equal(t, "replay.start", tt)
	mtyp, raw, _ = bConn.Read(context.Background())
	require.Equal(t, websocket.MessageText, mtyp)
	tt, _, _, _ = proto.Decode(raw)
	require.Equal(t, "replay.end", tt)

	// 9. Wrapper sends pty.data; browser receives.
	id, _ := ulid.ParseStrict(sessJSON.ID)
	_ = wConn.Write(context.Background(), websocket.MessageBinary, proto.EncodePTYData(id, []byte("hello!")))
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mtyp, raw, _ := bConn.Read(context.Background())
		if mtyp == websocket.MessageBinary {
			_, payload, _ := proto.DecodePTYData(raw)
			if string(payload) == "hello!" {
				return // success
			}
		}
	}
	t.Fatal("did not receive expected pty.data on browser ws")
}
```

Helper functions `newJar`, `stateCookie`, `csrfFromJar`, `sessionCookieFromJar` go in the same file (small wrappers around `net/http/cookiejar` and reading cookies). Implement as needed during the task.

- [ ] **Step 2: Run with the e2e tag**

```bash
go test -tags e2e ./cmd/claude-switch-server/... -timeout 5m
```

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add cmd/claude-switch-server
git commit -m "test(e2e): server full pair/connect/stream round-trip"
```

---

## Task 24: CI extension

**Goal:** Add a CI job that runs the server's testcontainer-backed tests on Linux, plus image build.

**Files:**
- Modify: `.github/workflows/ci.yml`

- [ ] **Step 1: Add a server-tests job**

In `.github/workflows/ci.yml`, append to the `jobs:` block:

```yaml
  server-tests:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.24' }
      - name: Run store + api + ws tests
        run: go test ./internal/store/... ./internal/api/... ./internal/oauth/... ./internal/hub/... ./internal/wswrapper/... ./internal/wsbrowser/... -timeout 10m
      - name: Run e2e (tag)
        run: go test -tags e2e ./cmd/claude-switch-server/... -timeout 10m

  server-image:
    runs-on: ubuntu-latest
    needs: server-tests
    steps:
      - uses: actions/checkout@v4
      - name: Build server image
        run: docker build -f Dockerfile.server -t claude-switch-server:ci .
```

- [ ] **Step 2: Commit + push to verify CI**

```bash
git add .github/workflows/ci.yml
git commit -m "ci: server tests + image build job"
git push origin master
```

Then watch the run with `gh run watch`. Iterate on any infra issues.

---

## Task 25: Wire up POST `/api/me/settings` retention setting (optional polish)

**Goal:** The Mongo TTL on `session_messages` is global at index creation time. To support the spec's "configurable per-user retention", add a per-user `transcript_retention_days` field to `users` and adjust the index creation strategy.

**Files:**
- Modify: `internal/store/users.go`
- Modify: `internal/store/store.go`
- Modify: `internal/api/me.go`

- [ ] **Step 1: Add field**

Append to `User` struct:

```go
TranscriptRetentionDays int `bson:"transcript_retention_days,omitempty"`
```

Add a setter on `UsersRepo`:

```go
func (r *UsersRepo) SetTranscriptRetention(ctx context.Context, id string, days int) error {
	_, err := r.coll.UpdateByID(ctx, objectIDFromHex(id), bson.M{
		"$set": bson.M{"transcript_retention_days": days},
	})
	return err
}
```

- [ ] **Step 2: Honor per-user retention at write time**

Per-user retention isn't enforceable via a single TTL index. The simplest approach: keep the global 90-day TTL as a hard ceiling; if a user wants longer, document that the global TTL still applies and they'd need to mirror to their own storage. For shorter retention, run a periodic cleanup job (out of scope MVP).

Document this in the `MeHandlers.UpdateSettings` JSON response by ignoring/clamping `transcript_retention_days` to ≤90.

Update `MeHandlers.UpdateSettings`:

```go
var body struct {
    KeepTranscripts          *bool `json:"keep_transcripts,omitempty"`
    TranscriptRetentionDays  *int  `json:"transcript_retention_days,omitempty"`
}
// ... existing decode + KeepTranscripts handling ...
if body.TranscriptRetentionDays != nil {
    days := *body.TranscriptRetentionDays
    if days < 1 {
        days = 1
    }
    if days > 90 {
        days = 90
    }
    if err := h.cfg.Store.Users().SetTranscriptRetention(r.Context(), u.ID, days); err != nil {
        http.Error(w, "store", http.StatusInternalServerError)
        return
    }
}
```

- [ ] **Step 3: Commit**

```bash
go test ./internal/store/... ./internal/api/... -timeout 120s
git add internal/store internal/api
git commit -m "feat(me): per-user transcript_retention_days (clamped 1-90)"
```

---

## Task 26: Tag and release v0.2.0

**Goal:** Tag the server release. The wrapper is already at v0.1.x; bumping the monorepo tag to v0.2.0 marks the addition of the server.

**Files:**
- Modify: `.goreleaser.yaml` (add server build)

- [ ] **Step 1: Add server to goreleaser**

In `.goreleaser.yaml`, add a second build entry:

```yaml
  - id: claude-switch-server
    main: ./cmd/claude-switch-server
    binary: claude-switch-server
    env:
      - CGO_ENABLED=0
    goos: [linux, darwin]
    goarch: [amd64, arm64]
```

Update the archives section so both binaries get included:

```yaml
archives:
  - id: default
    builds: [claude-switch, claude-switch-server]
    formats: [tar.gz]
    format_overrides:
      - goos: windows
        formats: [zip]
    name_template: "{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}"
```

- [ ] **Step 2: Tag + push**

```bash
go test ./...
git add .goreleaser.yaml
git commit -m "release: bundle claude-switch-server in goreleaser archives"
git push origin master

git tag v0.2.0
git push origin v0.2.0
```

Expected: GitHub release workflow builds and publishes both binaries; Docker Compose can pull the tagged image once the GHCR publishing step is added (follow-up).

---

## Final steps (post-plan execution)

After every task is green:

1. `go test -race ./...` (full suite locally).
2. `golangci-lint run ./...` (with the `.golangci.yml` v2 schema from subsystem 1).
3. Build the docker image: `make docker-server`.
4. Boot the compose stack against your existing Traefik+Mongo: `docker compose up -d --build`.
5. Pair a wrapper from a real machine and verify: `claude-switch pair https://claude-switch.dns.nom.es`.
6. Hand off to subsystem 3 (frontend). The contract is `docs/superpowers/specs/2026-04-25-server-design.md`'s API surface.

## Notes for the implementer

- **Do not add features not in the spec.** Rate limiting, push notifications, federated discovery — all out of scope for subsystem 2.
- **Tests must run on Linux CI (Mongo testcontainer requires Docker).** Skip cleanly on macOS/Windows runners by detecting Docker absence.
- **Every commit leaves the build green.** If you have to defer integrating a piece (e.g. the hub before WS handlers exist), use a stub that satisfies the same interface and tests still pass.
- **Keep files focused.** If a handler file grows past ~250 lines, split before committing.
- **Commit messages follow the imperative subject lines shown in each task.** ≤70 chars.
