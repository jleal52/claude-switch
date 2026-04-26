package store

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"golang.org/x/crypto/bcrypt"
)

var ErrRevoked = errors.New("store: wrapper revoked")

// wrapperDoc is the internal BSON-mapped struct for a wrapper document.
// The public Wrapper type exposes hex string IDs instead of ObjectIDs.
type wrapperDoc struct {
	OID              bson.ObjectID `bson:"_id,omitempty"`
	UserOID          bson.ObjectID `bson:"user_id"`
	Name             string        `bson:"name"`
	OS               string        `bson:"os"`
	Arch             string        `bson:"arch"`
	Version          string        `bson:"version"`
	PairedAt         time.Time     `bson:"paired_at"`
	LastSeenAt       time.Time     `bson:"last_seen_at"`
	RefreshTokenHash string        `bson:"refresh_token_hash"`
	RefreshTokenID   string        `bson:"refresh_token_id"`
	RevokedAt        *time.Time    `bson:"revoked_at,omitempty"`
}

func (d *wrapperDoc) toWrapper() *Wrapper {
	return &Wrapper{
		ID:               d.OID.Hex(),
		UserID:           d.UserOID.Hex(),
		Name:             d.Name,
		OS:               d.OS,
		Arch:             d.Arch,
		Version:          d.Version,
		PairedAt:         d.PairedAt,
		LastSeenAt:       d.LastSeenAt,
		RefreshTokenHash: d.RefreshTokenHash,
		RefreshTokenID:   d.RefreshTokenID,
		RevokedAt:        d.RevokedAt,
	}
}

// Wrapper is the public representation of a paired wrapper instance.
type Wrapper struct {
	ID               string
	UserID           string
	Name             string
	OS               string
	Arch             string
	Version          string
	PairedAt         time.Time
	LastSeenAt       time.Time
	RefreshTokenHash string
	RefreshTokenID   string
	RevokedAt        *time.Time
}

// WrapperCreate holds the fields needed to register a new wrapper.
type WrapperCreate struct {
	UserID  string
	Name    string
	OS      string
	Arch    string
	Version string
}

// WrappersRepo provides operations for wrapper documents.
type WrappersRepo struct{ coll *mongo.Collection }

// Wrappers returns a WrappersRepo for the store.
func (s *Store) Wrappers() *WrappersRepo { return &WrappersRepo{coll: s.db.Collection("wrappers")} }

// randomToken generates a URL-safe base64-encoded random token of nBytes entropy.
func randomToken(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Create registers a new wrapper for the given user. It returns the persisted
// Wrapper, the plaintext refresh token (format: "<token_id>.<random>"), and any error.
func (r *WrappersRepo) Create(ctx context.Context, in WrapperCreate) (*Wrapper, string, error) {
	tokenID, err := randomToken(16)
	if err != nil {
		return nil, "", err
	}
	secret, err := randomToken(32)
	if err != nil {
		return nil, "", err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.DefaultCost)
	if err != nil {
		return nil, "", err
	}
	now := time.Now().UTC()
	doc := wrapperDoc{
		UserOID:          objectIDFromHex(in.UserID),
		Name:             in.Name,
		OS:               in.OS,
		Arch:             in.Arch,
		Version:          in.Version,
		PairedAt:         now,
		LastSeenAt:       now,
		RefreshTokenHash: string(hash),
		RefreshTokenID:   tokenID,
	}
	res, err := r.coll.InsertOne(ctx, doc)
	if err != nil {
		return nil, "", err
	}
	doc.OID = res.InsertedID.(bson.ObjectID)
	plain := tokenID + "." + secret
	return doc.toWrapper(), plain, nil
}

// ListByUser returns all wrappers owned by the given user, ordered by paired_at desc.
func (r *WrappersRepo) ListByUser(ctx context.Context, userID string) ([]*Wrapper, error) {
	cur, err := r.coll.Find(ctx, bson.M{"user_id": objectIDFromHex(userID)})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []*Wrapper
	for cur.Next(ctx) {
		var d wrapperDoc
		if err := cur.Decode(&d); err != nil {
			return nil, err
		}
		out = append(out, d.toWrapper())
	}
	return out, cur.Err()
}

// VerifyRefreshToken validates a plaintext refresh token and returns the
// associated Wrapper. Returns ErrRevoked if the wrapper has been revoked,
// ErrNotFound if the token is invalid or unrecognised.
func (r *WrappersRepo) VerifyRefreshToken(ctx context.Context, plain string) (*Wrapper, error) {
	parts := strings.SplitN(plain, ".", 2)
	if len(parts) != 2 {
		return nil, ErrNotFound
	}
	tokenID, secret := parts[0], parts[1]

	var d wrapperDoc
	err := r.coll.FindOne(ctx, bson.M{"refresh_token_id": tokenID}).Decode(&d)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	if d.RevokedAt != nil {
		return nil, ErrRevoked
	}

	if err := bcrypt.CompareHashAndPassword([]byte(d.RefreshTokenHash), []byte(secret)); err != nil {
		return nil, ErrNotFound
	}

	return d.toWrapper(), nil
}

// Revoke marks a wrapper as revoked by setting revoked_at to now.
func (r *WrappersRepo) Revoke(ctx context.Context, id string) error {
	_, err := r.coll.UpdateByID(ctx, objectIDFromHex(id), bson.M{
		"$set": bson.M{"revoked_at": time.Now().UTC()},
	})
	return err
}

// UpdateLastSeen sets last_seen_at to now for the given wrapper.
func (r *WrappersRepo) UpdateLastSeen(ctx context.Context, id string) error {
	_, err := r.coll.UpdateByID(ctx, objectIDFromHex(id), bson.M{
		"$set": bson.M{"last_seen_at": time.Now().UTC()},
	})
	return err
}
