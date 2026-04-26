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

// WrapperDescriptor holds the identifying info about a wrapper requesting pairing.
type WrapperDescriptor struct {
	Name    string `bson:"name"`
	OS      string `bson:"os"`
	Arch    string `bson:"arch"`
	Version string `bson:"version"`
}

// pairingDoc is the internal BSON-mapped struct for a pairing_codes document.
// The public PairingCode type exposes hex string IDs instead of ObjectIDs.
type pairingDoc struct {
	OID       bson.ObjectID     `bson:"_id,omitempty"`
	UserOID   bson.ObjectID     `bson:"user_id,omitempty"`
	Code      string            `bson:"code"`
	Status    string            `bson:"status"`
	Wrapper   WrapperDescriptor `bson:"wrapper"`
	ExpiresAt time.Time         `bson:"expires_at"`
}

func (d *pairingDoc) toPairingCode() *PairingCode {
	pc := &PairingCode{
		ID:        d.OID.Hex(),
		Code:      d.Code,
		Status:    d.Status,
		Wrapper:   d.Wrapper,
		ExpiresAt: d.ExpiresAt,
	}
	// Only populate UserID if user_id was actually set (non-zero ObjectID).
	if d.UserOID != (bson.ObjectID{}) {
		pc.UserID = d.UserOID.Hex()
	}
	return pc
}

// PairingCode is the public representation of a pairing code document.
type PairingCode struct {
	// ID + UserID exposed as hex strings (or empty for unset). Internal
	// _id and user_id are ObjectIDs in Mongo.
	ID        string
	Code      string
	Status    string // pending | approved | denied
	UserID    string // empty until approved
	Wrapper   WrapperDescriptor
	ExpiresAt time.Time
}

// PairingRepo provides CRUD operations for pairing_codes documents.
type PairingRepo struct{ coll *mongo.Collection }

// Pairing returns a PairingRepo for the store.
func (s *Store) Pairing() *PairingRepo { return &PairingRepo{coll: s.db.Collection("pairing_codes")} }

// Create inserts a new pairing code with status="pending" and TTL.
func (r *PairingRepo) Create(ctx context.Context, w WrapperDescriptor, ttl time.Duration) (*PairingCode, error) {
	code, err := generatePairingCode()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	doc := pairingDoc{
		Code:      code,
		Status:    "pending",
		Wrapper:   w,
		ExpiresAt: now.Add(ttl),
	}
	res, err := r.coll.InsertOne(ctx, doc)
	if err != nil {
		return nil, err
	}
	doc.OID = res.InsertedID.(bson.ObjectID)
	return doc.toPairingCode(), nil
}

// GetByCode returns the pairing code document by code, or ErrNotFound.
func (r *PairingRepo) GetByCode(ctx context.Context, code string) (*PairingCode, error) {
	var d pairingDoc
	err := r.coll.FindOne(ctx, bson.M{"code": code}).Decode(&d)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return d.toPairingCode(), nil
}

// Approve flips status from pending->approved and sets user_id.
// Returns ErrAlreadyApproved if the row is no longer pending; ErrNotFound if missing.
func (r *PairingRepo) Approve(ctx context.Context, code string, userID string) error {
	res, err := r.coll.UpdateOne(ctx,
		bson.M{"code": code, "status": "pending"},
		bson.M{"$set": bson.M{
			"status":  "approved",
			"user_id": objectIDFromHex(userID),
		}},
	)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		// Distinguish "not pending" vs "missing".
		var d pairingDoc
		err2 := r.coll.FindOne(ctx, bson.M{"code": code}).Decode(&d)
		if errors.Is(err2, mongo.ErrNoDocuments) {
			return ErrNotFound
		}
		if err2 != nil {
			return err2
		}
		return ErrAlreadyApproved
	}
	return nil
}

// Delete removes the pairing code document by code.
func (r *PairingRepo) Delete(ctx context.Context, code string) error {
	_, err := r.coll.DeleteOne(ctx, bson.M{"code": code})
	return err
}

// generatePairingCode produces "ABCD-1234"-style codes (no ambiguous chars).
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
