package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// transcriptDoc is the BSON-mapped struct for the `transcripts` collection.
// Keyed on (wrapper_id, jsonl_uuid) which is unique per wrapper.
type transcriptDoc struct {
	OID          bson.ObjectID `bson:"_id,omitempty"`
	UserOID      bson.ObjectID `bson:"user_id"`
	WrapperOID   bson.ObjectID `bson:"wrapper_id"`
	ProjectOID   bson.ObjectID `bson:"project_id"`
	JSONLUUID    string        `bson:"jsonl_uuid"`
	Path         string        `bson:"path"`
	StartedAt    time.Time     `bson:"started_at"`
	EndedAt      time.Time     `bson:"ended_at"`
	MessageCount int           `bson:"message_count"`
	Title        string        `bson:"title"`
	Bytes        int64         `bson:"bytes"`
	DeletedAt    *time.Time    `bson:"deleted_at,omitempty"`
}

func (d *transcriptDoc) toTranscript() *Transcript {
	return &Transcript{
		ID:           d.OID.Hex(),
		UserID:       d.UserOID.Hex(),
		WrapperID:    d.WrapperOID.Hex(),
		ProjectID:    d.ProjectOID.Hex(),
		JSONLUUID:    d.JSONLUUID,
		Path:         d.Path,
		StartedAt:    d.StartedAt,
		EndedAt:      d.EndedAt,
		MessageCount: d.MessageCount,
		Title:        d.Title,
		Bytes:        d.Bytes,
		DeletedAt:    d.DeletedAt,
	}
}

// Transcript is the public representation of a transcripts document.
type Transcript struct {
	ID           string    // hex of ObjectID
	UserID       string    // hex of ObjectID
	WrapperID    string    // hex of ObjectID
	ProjectID    string    // hex of ObjectID
	JSONLUUID    string    // filename without .jsonl, unique per wrapper
	Path         string    // relative to ~/.claude/projects/
	StartedAt    time.Time
	EndedAt      time.Time
	MessageCount int
	Title        string
	Bytes        int64
	// DeletedAt is non-nil when the user soft-deleted the transcript via
	// the portal. List endpoints filter these out by default; the row
	// stays in the DB and survives wrapper-side catalog reconciliation.
	DeletedAt *time.Time
}

// TranscriptUpsert is the wrapper-side intent for a transcript row.
// ProjectSlug is resolved server-side to ProjectID via Projects().UpsertMany.
type TranscriptUpsert struct {
	JSONLUUID    string
	ProjectSlug  string
	Path         string
	StartedAt    time.Time
	EndedAt      time.Time
	MessageCount int
	Title        string
	Bytes        int64
}

type TranscriptsRepo struct {
	coll     *mongo.Collection
	projects *ProjectsRepo
}

func (s *Store) Transcripts() *TranscriptsRepo {
	return &TranscriptsRepo{
		coll:     s.db.Collection("transcripts"),
		projects: s.Projects(),
	}
}

// UpsertMany inserts or updates transcripts. slugToProjectID is the map
// returned by ProjectsRepo.UpsertMany; every transcript.ProjectSlug must
// resolve to an entry.
func (r *TranscriptsRepo) UpsertMany(ctx context.Context, userID, wrapperID string, slugToProjectID map[string]string, transcripts []TranscriptUpsert) error {
	if len(transcripts) == 0 {
		return nil
	}
	userOID, err := bson.ObjectIDFromHex(userID)
	if err != nil {
		return err
	}
	wrapperOID, err := bson.ObjectIDFromHex(wrapperID)
	if err != nil {
		return err
	}
	for _, t := range transcripts {
		pidHex, ok := slugToProjectID[t.ProjectSlug]
		if !ok {
			return fmt.Errorf("transcripts: unknown project slug %q (must upsert project first)", t.ProjectSlug)
		}
		pOID, err := bson.ObjectIDFromHex(pidHex)
		if err != nil {
			return err
		}
		filter := bson.M{"wrapper_id": wrapperOID, "jsonl_uuid": t.JSONLUUID}
		update := bson.M{
			"$set": bson.M{
				"project_id":    pOID,
				"path":          t.Path,
				"started_at":    t.StartedAt,
				"ended_at":      t.EndedAt,
				"message_count": t.MessageCount,
				"title":         t.Title,
				"bytes":         t.Bytes,
			},
			"$setOnInsert": bson.M{
				"user_id":     userOID,
				"wrapper_id":  wrapperOID,
				"jsonl_uuid":  t.JSONLUUID,
			},
		}
		opts := options.UpdateOne().SetUpsert(true)
		if _, err := r.coll.UpdateOne(ctx, filter, update, opts); err != nil {
			return err
		}
	}
	return nil
}

// DeleteByUUIDs removes transcripts of one wrapper by their jsonl_uuid.
func (r *TranscriptsRepo) DeleteByUUIDs(ctx context.Context, wrapperID string, uuids []string) error {
	if len(uuids) == 0 {
		return nil
	}
	wrapperOID, err := bson.ObjectIDFromHex(wrapperID)
	if err != nil {
		return err
	}
	_, err = r.coll.DeleteMany(ctx, bson.M{"wrapper_id": wrapperOID, "jsonl_uuid": bson.M{"$in": uuids}})
	return err
}

// ReplaceForWrapper atomically (per collection) reconciles both projects and
// transcripts to match the wrapper's full snapshot:
//
//  1. Upsert all projects, capture slug→id map.
//  2. Delete projects of this wrapper not in the new set.
//  3. Upsert all transcripts using the slug→id map.
//  4. Delete transcripts of this wrapper whose jsonl_uuid is not in the new set.
//
// Not wrapped in a single Mongo transaction; the wrapper's `catalog.diff
// full=true` is the only writer for its own slice of the catalog, so writer
// races are not a concern.
func (r *TranscriptsRepo) ReplaceForWrapper(ctx context.Context, userID, wrapperID string, projects []ProjectUpsert, transcripts []TranscriptUpsert) error {
	slugToID, err := r.projects.UpsertMany(ctx, userID, wrapperID, projects)
	if err != nil {
		return err
	}
	keepSlugs := make([]string, 0, len(projects))
	for _, p := range projects {
		keepSlugs = append(keepSlugs, p.Slug)
	}
	if err := r.projects.DeleteForWrapperExcept(ctx, wrapperID, keepSlugs); err != nil {
		return err
	}
	if err := r.UpsertMany(ctx, userID, wrapperID, slugToID, transcripts); err != nil {
		return err
	}
	keepUUIDs := make([]string, 0, len(transcripts))
	for _, t := range transcripts {
		keepUUIDs = append(keepUUIDs, t.JSONLUUID)
	}
	wrapperOID, err := bson.ObjectIDFromHex(wrapperID)
	if err != nil {
		return err
	}
	// Hard-delete orphans (transcripts the wrapper no longer mentions) BUT
	// preserve any that the user has soft-deleted via the portal — that
	// choice has to survive even if the user later removed the JSONL from
	// disk.
	filter := bson.M{
		"wrapper_id": wrapperOID,
		"$or": []bson.M{
			{"deleted_at": bson.M{"$exists": false}},
			{"deleted_at": nil},
		},
	}
	if len(keepUUIDs) > 0 {
		filter["jsonl_uuid"] = bson.M{"$nin": keepUUIDs}
	}
	_, err = r.coll.DeleteMany(ctx, filter)
	return err
}

// SoftDelete marks a transcript as deleted by the user. The row stays in
// the DB; list endpoints filter it out by default. Idempotent: a second
// call leaves deleted_at unchanged.
func (r *TranscriptsRepo) SoftDelete(ctx context.Context, id string) error {
	oid, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	res, err := r.coll.UpdateOne(ctx,
		bson.M{"_id": oid, "$or": []bson.M{
			{"deleted_at": bson.M{"$exists": false}},
			{"deleted_at": nil},
		}},
		bson.M{"$set": bson.M{"deleted_at": time.Now().UTC()}},
	)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		// Either not found, or already soft-deleted — verify existence so
		// the API can distinguish 404 from no-op.
		if err := r.coll.FindOne(ctx, bson.M{"_id": oid}).Err(); err != nil {
			if errors.Is(err, mongo.ErrNoDocuments) {
				return ErrNotFound
			}
			return err
		}
	}
	return nil
}

// ListByWrapper returns every transcript of one wrapper, most recent first.
func (r *TranscriptsRepo) ListByWrapper(ctx context.Context, wrapperID string, limit int) ([]*Transcript, error) {
	wrapperOID, err := bson.ObjectIDFromHex(wrapperID)
	if err != nil {
		return nil, err
	}
	return r.list(ctx, bson.M{"wrapper_id": wrapperOID}, limit)
}

// ListByProject returns every transcript of one project, most recent first.
func (r *TranscriptsRepo) ListByProject(ctx context.Context, projectID string, limit int) ([]*Transcript, error) {
	pOID, err := bson.ObjectIDFromHex(projectID)
	if err != nil {
		return nil, err
	}
	return r.list(ctx, bson.M{"project_id": pOID}, limit)
}

// ListRecentByUser returns the user's most recent transcripts across all
// wrappers.
func (r *TranscriptsRepo) ListRecentByUser(ctx context.Context, userID string, limit int) ([]*Transcript, error) {
	userOID, err := bson.ObjectIDFromHex(userID)
	if err != nil {
		return nil, err
	}
	return r.list(ctx, bson.M{"user_id": userOID}, limit)
}

// LiveUUIDsForUser filters jsonlUUIDs down to those that exist for the
// user AND are not soft-deleted. Used by /api/search to drop matches
// that point at transcripts the user has hidden.
func (r *TranscriptsRepo) LiveUUIDsForUser(ctx context.Context, userID string, jsonlUUIDs []string) ([]string, error) {
	if len(jsonlUUIDs) == 0 {
		return nil, nil
	}
	userOID, err := bson.ObjectIDFromHex(userID)
	if err != nil {
		return nil, err
	}
	filter := bson.M{
		"user_id":    userOID,
		"jsonl_uuid": bson.M{"$in": jsonlUUIDs},
		"$or": []bson.M{
			{"deleted_at": bson.M{"$exists": false}},
			{"deleted_at": nil},
		},
	}
	cur, err := r.coll.Find(ctx, filter, options.Find().SetProjection(bson.M{"jsonl_uuid": 1}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []string
	for cur.Next(ctx) {
		var d struct {
			JSONLUUID string `bson:"jsonl_uuid"`
		}
		if err := cur.Decode(&d); err != nil {
			return nil, err
		}
		out = append(out, d.JSONLUUID)
	}
	return out, nil
}

// GetByID looks up a transcript by its hex ObjectID.
func (r *TranscriptsRepo) GetByID(ctx context.Context, id string) (*Transcript, error) {
	oid, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return nil, err
	}
	var d transcriptDoc
	if err := r.coll.FindOne(ctx, bson.M{"_id": oid}).Decode(&d); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return d.toTranscript(), nil
}

func (r *TranscriptsRepo) list(ctx context.Context, filter bson.M, limit int) ([]*Transcript, error) {
	// Default behaviour: hide soft-deleted rows. Callers can pre-set
	// "deleted_at" in `filter` to override (e.g. include_deleted=true).
	if _, ok := filter["deleted_at"]; !ok {
		filter["$or"] = []bson.M{
			{"deleted_at": bson.M{"$exists": false}},
			{"deleted_at": nil},
		}
	}
	opts := options.Find().SetSort(bson.D{{Key: "started_at", Value: -1}})
	if limit > 0 {
		opts.SetLimit(int64(limit))
	}
	cur, err := r.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []*Transcript
	for cur.Next(ctx) {
		var d transcriptDoc
		if err := cur.Decode(&d); err != nil {
			return nil, err
		}
		out = append(out, d.toTranscript())
	}
	if err := cur.Err(); err != nil && !errors.Is(err, mongo.ErrNoDocuments) {
		return nil, err
	}
	return out, nil
}
