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

// objectIDFromHex converts a 24-hex-char string into a Mongo ObjectID.
// Returns the zero ObjectID if hex is empty or malformed; callers that
// care should validate first via bson.ObjectIDFromHex.
func objectIDFromHex(hex string) bson.ObjectID {
	id, _ := bson.ObjectIDFromHex(hex)
	return id
}

func (s *Store) ensureIndexes(ctx context.Context) error {
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
