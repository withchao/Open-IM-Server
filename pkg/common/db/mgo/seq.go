package mgo

import (
	"context"
	"errors"
	"github.com/OpenIMSDK/tools/errs"
	"github.com/OpenIMSDK/tools/log"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func NewSeq(db *mongo.Database) (*SeqMongo, error) {
	coll := db.Collection("seq")
	return &SeqMongo{coll: coll}, nil
}

type SeqMongo struct {
	coll *mongo.Collection
}

func (s *SeqMongo) MallocSeq(ctx context.Context, conversationID string, size int64) (int64, error) {
	if size <= 0 {
		return 0, errors.New("size must be greater than 0")
	}
	filter := map[string]any{"conversation_id": conversationID}
	update := map[string]any{
		"$inc": map[string]any{"seq": size},
	}
	opt := options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After).SetProjection(map[string]any{"_id": 0, "seq": 1})
	result := s.coll.FindOneAndUpdate(ctx, filter, update, opt)
	if err := result.Err(); err != nil {
		return 0, errs.Wrap(err)
	}
	var seqResult struct {
		Seq int64 `bson:"seq"`
	}
	if err := result.Decode(&seqResult); err != nil {
		return 0, errs.Wrap(err, "decode seq result")
	}
	log.ZDebug(ctx, "malloc seq call mongo", "conversationID", conversationID, "size", size, "startSeq", seqResult.Seq-size+1, "endSeq", seqResult.Seq)
	return seqResult.Seq, nil
}

func (s *SeqMongo) Malloc(ctx context.Context, conversationID string, size int64) ([]int64, error) {
	seq, err := s.MallocSeq(ctx, conversationID, size)
	if err != nil {
		return nil, err
	}
	seqs := make([]int64, 0, size)
	for i := seq - size + 1; i <= seq; i++ {
		seqs = append(seqs, i)
	}
	return seqs, nil
}
