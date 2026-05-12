package store

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// projectDoc is the BSON-mapped struct for the `projects` collection.
// _id is auto-allocated by Mongo and stable across upserts on
// (wrapper_id, slug). user_id and wrapper_id are stored as ObjectIDs.
type projectDoc struct {
	OID             bson.ObjectID `bson:"_id,omitempty"`
	UserOID         bson.ObjectID `bson:"user_id"`
	WrapperOID      bson.ObjectID `bson:"wrapper_id"`
	Slug            string        `bson:"slug"`
	Cwd             string        `bson:"cwd"`
	Name            string        `bson:"name"`
	SessionCount    int           `bson:"session_count"`
	FirstActivityAt time.Time     `bson:"first_activity_at"`
	LastActivityAt  time.Time     `bson:"last_activity_at"`
}

func (d *projectDoc) toProject() *Project {
	return &Project{
		ID:              d.OID.Hex(),
		UserID:          d.UserOID.Hex(),
		WrapperID:       d.WrapperOID.Hex(),
		Slug:            d.Slug,
		Cwd:             d.Cwd,
		Name:            d.Name,
		SessionCount:    d.SessionCount,
		FirstActivityAt: d.FirstActivityAt,
		LastActivityAt:  d.LastActivityAt,
	}
}

// Project is the public representation of a project document. One row per
// (wrapper, project-slug) tuple.
type Project struct {
	ID              string    // hex of ObjectID
	UserID          string    // hex of ObjectID
	WrapperID       string    // hex of ObjectID
	Slug            string    // dir name under ~/.claude/projects/
	Cwd             string    // absolute cwd recovered from JSONL events
	Name            string    // basename of Cwd
	SessionCount    int
	FirstActivityAt time.Time
	LastActivityAt  time.Time
}

// ProjectUpsert is the wrapper-side intent for a project row.
type ProjectUpsert struct {
	Slug            string
	Cwd             string
	Name            string
	SessionCount    int
	FirstActivityAt time.Time
	LastActivityAt  time.Time
}

type ProjectsRepo struct{ coll *mongo.Collection }

func (s *Store) Projects() *ProjectsRepo { return &ProjectsRepo{coll: s.db.Collection("projects")} }

// UpsertMany inserts new projects or replaces fields on existing ones, keyed
// on (wrapper_id, slug). Returns slug → projectID (hex) for the caller to
// resolve transcript foreign keys.
func (r *ProjectsRepo) UpsertMany(ctx context.Context, userID, wrapperID string, projects []ProjectUpsert) (map[string]string, error) {
	if len(projects) == 0 {
		return map[string]string{}, nil
	}
	userOID, err := bson.ObjectIDFromHex(userID)
	if err != nil {
		return nil, err
	}
	wrapperOID, err := bson.ObjectIDFromHex(wrapperID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(projects))
	for _, p := range projects {
		filter := bson.M{"wrapper_id": wrapperOID, "slug": p.Slug}
		update := bson.M{
			"$set": bson.M{
				"cwd":               p.Cwd,
				"name":              p.Name,
				"session_count":     p.SessionCount,
				"first_activity_at": p.FirstActivityAt,
				"last_activity_at":  p.LastActivityAt,
			},
			"$setOnInsert": bson.M{
				"user_id":    userOID,
				"wrapper_id": wrapperOID,
				"slug":       p.Slug,
			},
		}
		opts := options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After)
		var d projectDoc
		if err := r.coll.FindOneAndUpdate(ctx, filter, update, opts).Decode(&d); err != nil {
			return nil, err
		}
		out[p.Slug] = d.OID.Hex()
	}
	return out, nil
}

// DeleteForWrapperExcept removes every project of wrapperID whose slug is not
// in keepSlugs. Idempotent.
func (r *ProjectsRepo) DeleteForWrapperExcept(ctx context.Context, wrapperID string, keepSlugs []string) error {
	wrapperOID, err := bson.ObjectIDFromHex(wrapperID)
	if err != nil {
		return err
	}
	filter := bson.M{"wrapper_id": wrapperOID}
	if len(keepSlugs) > 0 {
		filter["slug"] = bson.M{"$nin": keepSlugs}
	}
	_, err = r.coll.DeleteMany(ctx, filter)
	return err
}

// ListByUser returns every project owned by the user, sorted by last
// activity (most recent first).
func (r *ProjectsRepo) ListByUser(ctx context.Context, userID string) ([]*Project, error) {
	userOID, err := bson.ObjectIDFromHex(userID)
	if err != nil {
		return nil, err
	}
	return r.list(ctx, bson.M{"user_id": userOID})
}

// ListByWrapper returns every project of one wrapper, sorted by last
// activity (most recent first).
func (r *ProjectsRepo) ListByWrapper(ctx context.Context, wrapperID string) ([]*Project, error) {
	wrapperOID, err := bson.ObjectIDFromHex(wrapperID)
	if err != nil {
		return nil, err
	}
	return r.list(ctx, bson.M{"wrapper_id": wrapperOID})
}

func (r *ProjectsRepo) list(ctx context.Context, filter bson.M) ([]*Project, error) {
	opts := options.Find().SetSort(bson.D{{Key: "last_activity_at", Value: -1}})
	cur, err := r.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []*Project
	for cur.Next(ctx) {
		var d projectDoc
		if err := cur.Decode(&d); err != nil {
			return nil, err
		}
		out = append(out, d.toProject())
	}
	if err := cur.Err(); err != nil && !errors.Is(err, mongo.ErrNoDocuments) {
		return nil, err
	}
	return out, nil
}
