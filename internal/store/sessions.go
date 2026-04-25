package store

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// sessionDoc is the internal BSON-mapped struct for a sessions document.
// _id is a ULID string (NOT an ObjectID). user_id and wrapper_id are ObjectIDs.
type sessionDoc struct {
	ID         string        `bson:"_id"`
	UserOID    bson.ObjectID `bson:"user_id"`
	WrapperOID bson.ObjectID `bson:"wrapper_id"`
	JSONLUUID  string        `bson:"jsonl_uuid,omitempty"`
	Cwd        string        `bson:"cwd"`
	Account    string        `bson:"account"`
	Status     string        `bson:"status"`
	CreatedAt  time.Time     `bson:"created_at"`
	ExitedAt   *time.Time    `bson:"exited_at,omitempty"`
	ExitCode   *int          `bson:"exit_code,omitempty"`
	ExitReason string        `bson:"exit_reason,omitempty"`
}

func (d *sessionDoc) toSession() *Session {
	return &Session{
		ID:         d.ID,
		UserID:     d.UserOID.Hex(),
		WrapperID:  d.WrapperOID.Hex(),
		JSONLUUID:  d.JSONLUUID,
		Cwd:        d.Cwd,
		Account:    d.Account,
		Status:     d.Status,
		CreatedAt:  d.CreatedAt,
		ExitedAt:   d.ExitedAt,
		ExitCode:   d.ExitCode,
		ExitReason: d.ExitReason,
	}
}

// Session is the public representation of a session document.
type Session struct {
	ID         string
	UserID     string     // hex string of ObjectID
	WrapperID  string     // hex string of ObjectID
	JSONLUUID  string
	Cwd        string
	Account    string
	Status     string // starting | running | exited | wrapper_offline
	CreatedAt  time.Time
	ExitedAt   *time.Time
	ExitCode   *int
	ExitReason string
}

// SessionCreate holds the fields needed to create a new session.
type SessionCreate struct {
	ID        string
	UserID    string
	WrapperID string
	Cwd       string
	Account   string
}

// SessionsRepo provides CRUD operations for sessions documents.
type SessionsRepo struct{ coll *mongo.Collection }

// Sessions returns a SessionsRepo for the store.
func (s *Store) Sessions() *SessionsRepo { return &SessionsRepo{coll: s.db.Collection("sessions")} }

// Create inserts a new session with status="starting".
func (r *SessionsRepo) Create(ctx context.Context, in SessionCreate) (*Session, error) {
	now := time.Now().UTC()
	doc := sessionDoc{
		ID:         in.ID,
		UserOID:    objectIDFromHex(in.UserID),
		WrapperOID: objectIDFromHex(in.WrapperID),
		Cwd:        in.Cwd,
		Account:    in.Account,
		Status:     "starting",
		CreatedAt:  now,
	}
	_, err := r.coll.InsertOne(ctx, doc)
	if err != nil {
		return nil, err
	}
	return doc.toSession(), nil
}

// GetByID returns the session with the given ULID string ID, or ErrNotFound.
func (r *SessionsRepo) GetByID(ctx context.Context, id string) (*Session, error) {
	var d sessionDoc
	err := r.coll.FindOne(ctx, bson.M{"_id": id}).Decode(&d)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return d.toSession(), nil
}

// MarkRunning transitions a session to status="running" and records the JSONL UUID.
func (r *SessionsRepo) MarkRunning(ctx context.Context, id, jsonlUUID string) error {
	_, err := r.coll.UpdateOne(ctx,
		bson.M{"_id": id},
		bson.M{"$set": bson.M{
			"status":     "running",
			"jsonl_uuid": jsonlUUID,
		}},
	)
	return err
}

// MarkExited transitions a session to status="exited" and records exit info.
func (r *SessionsRepo) MarkExited(ctx context.Context, id string, exitCode int, reason, detail string) error {
	now := time.Now().UTC()
	_ = detail // detail reserved for future use
	_, err := r.coll.UpdateOne(ctx,
		bson.M{"_id": id},
		bson.M{"$set": bson.M{
			"status":      "exited",
			"exited_at":   now,
			"exit_code":   exitCode,
			"exit_reason": reason,
		}},
	)
	return err
}

// MarkWrapperOffline sets status="wrapper_offline" for all starting/running sessions
// on the given wrapper. Returns the number of documents updated.
func (r *SessionsRepo) MarkWrapperOffline(ctx context.Context, wrapperID string) (int64, error) {
	res, err := r.coll.UpdateMany(ctx,
		bson.M{
			"wrapper_id": objectIDFromHex(wrapperID),
			"status":     bson.M{"$in": bson.A{"starting", "running"}},
		},
		bson.M{"$set": bson.M{"status": "wrapper_offline"}},
	)
	if err != nil {
		return 0, err
	}
	return res.ModifiedCount, nil
}

// MarkRunningFromOffline transitions a single wrapper_offline session back to running.
func (r *SessionsRepo) MarkRunningFromOffline(ctx context.Context, id string) error {
	_, err := r.coll.UpdateOne(ctx,
		bson.M{"_id": id, "status": "wrapper_offline"},
		bson.M{"$set": bson.M{"status": "running"}},
	)
	return err
}

// ListByUser returns sessions for the given user, optionally filtered by status.
// statusFilter: "" = all, "live" = starting|running|wrapper_offline, "exited" = exited.
// Results are sorted by created_at descending.
func (r *SessionsRepo) ListByUser(ctx context.Context, userID, statusFilter string) ([]Session, error) {
	filter := bson.M{"user_id": objectIDFromHex(userID)}
	switch statusFilter {
	case "live":
		filter["status"] = bson.M{"$in": bson.A{"starting", "running", "wrapper_offline"}}
	case "exited":
		filter["status"] = "exited"
	}

	opts := options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}})
	cur, err := r.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	var out []Session
	for cur.Next(ctx) {
		var d sessionDoc
		if err := cur.Decode(&d); err != nil {
			return nil, err
		}
		out = append(out, *d.toSession())
	}
	return out, cur.Err()
}
