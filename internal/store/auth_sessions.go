package store

import (
	"context"
	"errors"
	"time"

	"github.com/oklog/ulid/v2"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// authSessionDoc is the internal BSON-mapped struct for an auth_sessions document.
// _id is a ULID string (cookie value). user_id is an ObjectID.
type authSessionDoc struct {
	ID        string        `bson:"_id"`
	UserOID   bson.ObjectID `bson:"user_id"`
	CSRFToken string        `bson:"csrf_token"`
	CreatedAt time.Time     `bson:"created_at"`
	LastSeen  time.Time     `bson:"last_seen"`
	ExpiresAt time.Time     `bson:"expires_at"`
}

func (d *authSessionDoc) toAuthSession() *AuthSession {
	return &AuthSession{
		ID:        d.ID,
		UserID:    d.UserOID.Hex(),
		CSRFToken: d.CSRFToken,
		CreatedAt: d.CreatedAt,
		LastSeen:  d.LastSeen,
		ExpiresAt: d.ExpiresAt,
	}
}

// AuthSession is the public representation of a browser auth session.
type AuthSession struct {
	ID        string // ULID, used as cookie value
	UserID    string // hex of ObjectID
	CSRFToken string
	CreatedAt time.Time
	LastSeen  time.Time
	ExpiresAt time.Time
}

// AuthSessionsRepo provides CRUD operations for auth_sessions documents.
type AuthSessionsRepo struct{ coll *mongo.Collection }

// AuthSessions returns an AuthSessionsRepo for the store.
func (s *Store) AuthSessions() *AuthSessionsRepo {
	return &AuthSessionsRepo{coll: s.db.Collection("auth_sessions")}
}

// Create inserts a new auth session for the given user with the specified TTL.
func (r *AuthSessionsRepo) Create(ctx context.Context, userID string, ttl time.Duration) (*AuthSession, error) {
	csrf, err := randomToken(24)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	doc := authSessionDoc{
		ID:        ulid.Make().String(),
		UserOID:   objectIDFromHex(userID),
		CSRFToken: csrf,
		CreatedAt: now,
		LastSeen:  now,
		ExpiresAt: now.Add(ttl),
	}
	_, err = r.coll.InsertOne(ctx, doc)
	if err != nil {
		return nil, err
	}
	return doc.toAuthSession(), nil
}

// GetByID returns the auth session with the given ULID string ID, or ErrNotFound
// if the session is missing or expired.
func (r *AuthSessionsRepo) GetByID(ctx context.Context, id string) (*AuthSession, error) {
	var d authSessionDoc
	err := r.coll.FindOne(ctx, bson.M{
		"_id":        id,
		"expires_at": bson.M{"$gt": time.Now().UTC()},
	}).Decode(&d)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return d.toAuthSession(), nil
}

// Delete removes the auth session with the given ID.
func (r *AuthSessionsRepo) Delete(ctx context.Context, id string) error {
	_, err := r.coll.DeleteOne(ctx, bson.M{"_id": id})
	return err
}

// Touch updates the last_seen and expires_at fields of the given session.
func (r *AuthSessionsRepo) Touch(ctx context.Context, id string, ttl time.Duration) error {
	now := time.Now().UTC()
	_, err := r.coll.UpdateOne(ctx,
		bson.M{"_id": id},
		bson.M{"$set": bson.M{
			"last_seen":  now,
			"expires_at": now.Add(ttl),
		}},
	)
	return err
}
