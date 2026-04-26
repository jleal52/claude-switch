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
	OID                     bson.ObjectID `bson:"_id,omitempty"`
	ID                      string        `bson:"-"`
	OAuthProvider           string        `bson:"oauth_provider"`
	OAuthSubject            string        `bson:"oauth_subject"`
	Email                   string        `bson:"email,omitempty"`
	Name                    string        `bson:"name,omitempty"`
	AvatarURL               string        `bson:"avatar_url,omitempty"`
	KeepTranscripts         bool          `bson:"keep_transcripts"`
	TranscriptRetentionDays int           `bson:"transcript_retention_days,omitempty"`
	CreatedAt               time.Time     `bson:"created_at"`
	LastLoginAt             time.Time     `bson:"last_login_at"`
}

// populateID fills the string ID field from the bson ObjectID.
func (u *User) populateID() *User {
	u.ID = u.OID.Hex()
	return u
}

// OAuthProfile is what an OAuth provider gives us after callback.
type OAuthProfile struct {
	Provider  string
	Subject   string
	Email     string
	Name      string
	AvatarURL string
}

// UsersRepo provides CRUD operations for users.
type UsersRepo struct{ coll *mongo.Collection }

// Users returns a UsersRepo for the store.
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
	return u.populateID(), nil
}

// GetByID fetches a user by hex ID. Returns ErrNotFound if absent.
func (r *UsersRepo) GetByID(ctx context.Context, id string) (*User, error) {
	var u User
	err := r.coll.FindOne(ctx, bson.M{"_id": objectIDFromHex(id)}).Decode(&u)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return u.populateID(), nil
}

// MarkLogin updates last_login_at to now.
func (r *UsersRepo) MarkLogin(ctx context.Context, id string) error {
	_, err := r.coll.UpdateByID(ctx, objectIDFromHex(id), bson.M{
		"$set": bson.M{"last_login_at": time.Now().UTC()},
	})
	return err
}

// SetKeepTranscripts updates the keep_transcripts flag.
func (r *UsersRepo) SetKeepTranscripts(ctx context.Context, id string, v bool) error {
	_, err := r.coll.UpdateByID(ctx, objectIDFromHex(id), bson.M{
		"$set": bson.M{"keep_transcripts": v},
	})
	return err
}

// SetTranscriptRetention persists the user's preferred retention window in
// days. Note: the actual TTL on session_messages is global (90 days at index
// creation); this setting is clamped to 1-90 by callers and is informational
// for shorter retention (which would require a periodic cleanup job, not in MVP).
func (r *UsersRepo) SetTranscriptRetention(ctx context.Context, id string, days int) error {
	_, err := r.coll.UpdateByID(ctx, objectIDFromHex(id), bson.M{
		"$set": bson.M{"transcript_retention_days": days},
	})
	return err
}
