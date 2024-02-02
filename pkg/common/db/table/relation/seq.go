package relation

import "context"

type Seq struct {
	ConversationID string `bson:"conversation_id"`
	Seq            int64  `bson:"seq"`
}

type SeqModelInterface interface {
	Malloc(ctx context.Context, conversationID string, size int64) ([]int64, error)
}
