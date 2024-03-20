package relation

import "context"

type SeqUser struct {
	ConversationID string `bson:"conversation_id"`
	UserID         string `bson:"user_id"`
	MaxSeq         int64  `bson:"max_seq"`
	MinSeq         int64  `bson:"min_seq"`
	ReadSeq        int64  `bson:"read_seq"`
}

type SeqUserModelInterface interface {
	SetReadSeq(ctx context.Context, conversationID string, userID string, seq int64) error
	SetMaxSeq(ctx context.Context, conversationID string, userID string, seq int64) error
	SetMinSeq(ctx context.Context, conversationID string, userID string, seq int64) error
	GetReadSeq(ctx context.Context, conversationID string, userID string) (int64, error)
	GetMaxSeq(ctx context.Context, conversationID string, userID string) (int64, error)
	GetMinSeq(ctx context.Context, conversationID string, userID string) (int64, error)
}
