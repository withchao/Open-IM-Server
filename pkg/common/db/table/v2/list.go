package v2

import (
	"context"
	"go.mongodb.org/mongo-driver/mongo"
	"time"
)

type List[T any] struct {
	QueryID    string    `bson:"query_id"`
	Elems      []T       `bson:"elems"`
	UpdateTime time.Time `bson:"update_time"`
}

type ListInterface[T any] interface {
	IDName() string
	ElemsName() string
	UpdateTimeName() string
	ElemUpdateTimeName() string
	ElemDeletedName() string
}

type Friend struct {
	UserID     string       `bson:"user_id"`
	Friends    []FriendElem `bson:"friends"`
	UpdateTime time.Time    `bson:"update_time"`
}

type FriendElem struct {
	UserID string `bson:"user_id"`
}

type listModel[T any] struct {
	coll *mongo.Collection
}

func (l *listModel[T]) Find(ctx context.Context) (*List[T], error) {

	return nil, nil
}
