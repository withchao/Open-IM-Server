package mgo

import (
	"context"
	"github.com/OpenIMSDK/tools/errs"
	"github.com/OpenIMSDK/tools/mgoutil"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func NewSeqUser(db *mongo.Database) (*SeqUserMongo, error) {
	coll := db.Collection("seq_user")
	return &SeqUserMongo{coll: coll}, nil
}

type SeqUserMongo struct {
	coll *mongo.Collection
}

func (s *SeqUserMongo) SetReadSeq(ctx context.Context, conversationID string, userID string, seq int64) error {
	return s.setSeq(ctx, conversationID, userID, "read_seq", seq)
}

func (s *SeqUserMongo) SetMaxSeq(ctx context.Context, conversationID string, userID string, seq int64) error {
	return s.setSeq(ctx, conversationID, userID, "max_seq", seq)
}

func (s *SeqUserMongo) SetMinSeq(ctx context.Context, conversationID string, userID string, seq int64) error {
	return s.setSeq(ctx, conversationID, userID, "min_seq", seq)
}

func (s *SeqUserMongo) GetReadSeq(ctx context.Context, conversationID string, userID string) (int64, error) {
	return s.getSeq(ctx, conversationID, userID, "read_seq")
}

func (s *SeqUserMongo) GetMaxSeq(ctx context.Context, conversationID string, userID string) (int64, error) {
	return s.getSeq(ctx, conversationID, userID, "max_seq")
}

func (s *SeqUserMongo) GetMinSeq(ctx context.Context, conversationID string, userID string) (int64, error) {
	return s.getSeq(ctx, conversationID, userID, "min_seq")
}

func (s *SeqUserMongo) setSeq(ctx context.Context, conversationID string, userID string, field string, seq int64) error {
	filter := map[string]any{"conversation_id": conversationID, "user_id": userID}
	update := map[string]any{
		"$set": map[string]any{field: seq},
	}
	return mgoutil.UpdateOne(ctx, s.coll, filter, update, false, options.Update().SetUpsert(true))
}

func (s *SeqUserMongo) getSeq(ctx context.Context, conversationID string, userID string, field string) (int64, error) {
	filter := map[string]any{"conversation_id": conversationID, "user_id": userID}
	opt := options.FindOne().SetProjection(bson.M{"_id": 0, field: 1})
	res, err := mgoutil.FindOne[int64](ctx, s.coll, filter, opt)
	if err != nil {
		if errs.Unwrap(err) == mongo.ErrNoDocuments {
			return 0, nil
		}
		return 0, err
	}
	return res, nil
}
