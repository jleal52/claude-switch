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

// wrapperAccessTokenDoc is the internal BSON-mapped struct for an access token document.
type wrapperAccessTokenDoc struct {
	OID        bson.ObjectID `bson:"_id,omitempty"`
	WrapperOID bson.ObjectID `bson:"wrapper_id"`
	UserOID    bson.ObjectID `bson:"user_id"`
	TokenHash  string        `bson:"token_hash"`
	ExpiresAt  time.Time     `bson:"expires_at"`
}

func (d *wrapperAccessTokenDoc) toAccessToken() *WrapperAccessToken {
	return &WrapperAccessToken{
		ID:        d.OID.Hex(),
		WrapperID: d.WrapperOID.Hex(),
		UserID:    d.UserOID.Hex(),
		TokenHash: d.TokenHash,
		ExpiresAt: d.ExpiresAt,
	}
}

// WrapperAccessToken is the public representation of a short-lived access token.
type WrapperAccessToken struct {
	ID        string
	WrapperID string
	UserID    string
	TokenHash string
	ExpiresAt time.Time
}

// WrapperTokensRepo provides operations for wrapper access token documents.
type WrapperTokensRepo struct{ coll *mongo.Collection }

// WrapperTokens returns a WrapperTokensRepo for the store.
func (s *Store) WrapperTokens() *WrapperTokensRepo {
	return &WrapperTokensRepo{coll: s.db.Collection("wrapper_access_tokens")}
}

// Issue creates a new access token for a wrapper and returns the plaintext token
// and its expiry time.
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

// Verify looks up an access token by its sha256 hash. Returns ErrNotFound if
// the token is absent or expired.
func (r *WrapperTokensRepo) Verify(ctx context.Context, plain string) (*WrapperAccessToken, error) {
	var d wrapperAccessTokenDoc
	err := r.coll.FindOne(ctx, bson.M{
		"token_hash": hashToken(plain),
		"expires_at": bson.M{"$gt": time.Now().UTC()},
	}).Decode(&d)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return d.toAccessToken(), nil
}

// hashToken returns the hex-encoded sha256 hash of the plaintext token.
func hashToken(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}
