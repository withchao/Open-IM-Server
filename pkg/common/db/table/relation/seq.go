package relation

import "context"

type Seq struct {
	ConversationID string `bson:"conversation_id"`
	MaxSeq         int64  `bson:"max_seq"`
	MinSeq         int64  `bson:"min_seq"`
}

type SeqModelInterface interface {
	Malloc(ctx context.Context, conversationID string, size int64) ([]int64, error)
	GetMaxSeq(ctx context.Context, conversationID string) (int64, error)
	GetMinSeq(ctx context.Context, conversationID string) (int64, error)
	SetMinSeq(ctx context.Context, conversationID string, seq int64) error
}
