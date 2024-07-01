package mgo

import (
	"context"
	"errors"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database"
	"github.com/openimsdk/tools/db/mongoutil"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func NewSeqUserMongo(db *mongo.Database) (database.SeqUser, error) {
	coll := db.Collection(database.SeqUserName)
	_, err := coll.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys: bson.D{
			{Key: "user_id", Value: 1},
			{Key: "conversation_id", Value: 1},
		},
	})
	if err != nil {
		return nil, err
	}
	return &seqUserMongo{coll: coll}, nil
}

type seqUserMongo struct {
	coll *mongo.Collection
}

func (s *seqUserMongo) setSeq(ctx context.Context, conversationID string, userID string, seq int64, field string) error {
	filter := map[string]any{
		"user_id":         userID,
		"conversation_id": conversationID,
	}
	insert := bson.M{
		"user_id":         userID,
		"conversation_id": conversationID,
		"min_seq":         0,
		"max_seq":         0,
		"read_seq":        0,
	}
	delete(insert, field)
	update := map[string]any{
		"$set": bson.M{
			field: seq,
		},
		"$setOnInsert": insert,
	}
	opt := options.Update().SetUpsert(true)
	return mongoutil.UpdateOne(ctx, s.coll, filter, update, false, opt)
}

func (s *seqUserMongo) getSeq(ctx context.Context, conversationID string, userID string, failed string) (int64, error) {
	filter := map[string]any{
		"user_id":         userID,
		"conversation_id": conversationID,
	}
	opt := options.FindOne().SetProjection(bson.M{"_id": 0, failed: 1})
	seq, err := mongoutil.FindOne[int64](ctx, s.coll, filter, opt)
	if err == nil {
		return seq, nil
	} else if errors.Is(err, mongo.ErrNoDocuments) {
		return 0, nil
	} else {
		return 0, err
	}
}

func (s *seqUserMongo) GetMaxSeq(ctx context.Context, conversationID string, userID string) (int64, error) {
	return s.getSeq(ctx, conversationID, userID, "max_seq")
}

func (s *seqUserMongo) SetMaxSeq(ctx context.Context, conversationID string, userID string, seq int64) error {
	return s.setSeq(ctx, conversationID, userID, seq, "max_seq")
}

func (s *seqUserMongo) GetMinSeq(ctx context.Context, conversationID string, userID string) (int64, error) {
	return s.getSeq(ctx, conversationID, userID, "min_seq")
}

func (s *seqUserMongo) SetMinSeq(ctx context.Context, conversationID string, userID string, seq int64) error {
	return s.setSeq(ctx, conversationID, userID, seq, "min_seq")
}

func (s *seqUserMongo) GetReadSeq(ctx context.Context, conversationID string, userID string) (int64, error) {
	return s.getSeq(ctx, conversationID, userID, "read_seq")
}

func (s *seqUserMongo) SetReadSeq(ctx context.Context, conversationID string, userID string, seq int64) error {
	return s.setSeq(ctx, conversationID, userID, seq, "read_seq")
}