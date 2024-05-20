// Copyright © 2023 OpenIM. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package mgo

import (
	"context"
	"github.com/openimsdk/tools/errs"

	"github.com/openimsdk/open-im-server/v3/pkg/common/db/table/relation"
	"github.com/openimsdk/tools/db/mongoutil"
	"github.com/openimsdk/tools/db/pagination"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// FriendMgo implements FriendModelInterface using MongoDB as the storage backend.
type FriendMgo struct {
	coll *mongo.Collection
}

// NewFriendMongo creates a new instance of FriendMgo with the provided MongoDB database.
func NewFriendMongo(db *mongo.Database) (relation.FriendModelInterface, error) {
	coll := db.Collection("friend")
	_, err := coll.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys: bson.D{
			{Key: "owner_user_id", Value: 1},
			{Key: "friend_user_id", Value: 1},
			{Key: "nickname", Value: 1},
		},
		Options: options.Index().SetUnique(true),
	})
	if err != nil {
		return nil, err
	}
	return &FriendMgo{coll: coll}, nil
}

// Create inserts multiple friend records.
func (f *FriendMgo) Create(ctx context.Context, friends []*relation.FriendModel) error {
	return mongoutil.InsertMany(ctx, f.coll, friends)
}

// Delete removes specified friends of the owner user.
func (f *FriendMgo) Delete(ctx context.Context, ownerUserID string, friendUserIDs []string) error {
	filter := bson.M{
		"owner_user_id":  ownerUserID,
		"friend_user_id": bson.M{"$in": friendUserIDs},
	}
	return mongoutil.DeleteOne(ctx, f.coll, filter)
}

// UpdateByMap updates specific fields of a friend document using a map.
func (f *FriendMgo) UpdateByMap(ctx context.Context, ownerUserID string, friendUserID string, args map[string]interface{}) error {
	if len(args) == 0 {
		return nil
	}
	filter := bson.M{
		"owner_user_id":  ownerUserID,
		"friend_user_id": friendUserID,
	}
	return mongoutil.UpdateOne(ctx, f.coll, filter, bson.M{"$set": args}, true)
}

// UpdateRemark updates the remark for a specific friend.
func (f *FriendMgo) UpdateRemark(ctx context.Context, ownerUserID, friendUserID, remark string) error {
	return f.UpdateByMap(ctx, ownerUserID, friendUserID, map[string]any{"remark": remark})
}

// Take retrieves a single friend document. Returns an error if not found.
func (f *FriendMgo) Take(ctx context.Context, ownerUserID, friendUserID string) (*relation.FriendModel, error) {
	filter := bson.M{
		"owner_user_id":  ownerUserID,
		"friend_user_id": friendUserID,
	}
	return mongoutil.FindOne[*relation.FriendModel](ctx, f.coll, filter)
}

// FindUserState finds the friendship status between two users.
func (f *FriendMgo) FindUserState(ctx context.Context, userID1, userID2 string) ([]*relation.FriendModel, error) {
	filter := bson.M{
		"$or": []bson.M{
			{"owner_user_id": userID1, "friend_user_id": userID2},
			{"owner_user_id": userID2, "friend_user_id": userID1},
		},
	}
	return mongoutil.Find[*relation.FriendModel](ctx, f.coll, filter)
}

// FindFriends retrieves a list of friends for a given owner. Missing friends do not cause an error.
func (f *FriendMgo) FindFriends(ctx context.Context, ownerUserID string, friendUserIDs []string) ([]*relation.FriendModel, error) {
	filter := bson.M{
		"owner_user_id":  ownerUserID,
		"friend_user_id": bson.M{"$in": friendUserIDs},
	}
	return mongoutil.Find[*relation.FriendModel](ctx, f.coll, filter)
}

// FindReversalFriends finds users who have added the specified user as a friend.
func (f *FriendMgo) FindReversalFriends(ctx context.Context, friendUserID string, ownerUserIDs []string) ([]*relation.FriendModel, error) {
	filter := bson.M{
		"owner_user_id":  bson.M{"$in": ownerUserIDs},
		"friend_user_id": friendUserID,
	}
	return mongoutil.Find[*relation.FriendModel](ctx, f.coll, filter)
}

// FindOwnerFriends retrieves a paginated list of friends for a given owner.
func (f *FriendMgo) FindOwnerFriends(ctx context.Context, ownerUserID string, pagination pagination.Pagination) (int64, []*relation.FriendModel, error) {
	where := bson.M{
		"owner_user_id": ownerUserID,
	}
	return f.aggregatePagination(ctx, where, pagination)
	//filter := bson.M{"owner_user_id": ownerUserID}
	//opt := options.Find().SetSort(bson.A{bson.M{"nickname": 1}, bson.M{"create_time": 1}})
	//return mongoutil.FindPage[*relation.FriendModel](ctx, f.coll, filter, pagination, opt)
}

// FindInWhoseFriends finds users who have added the specified user as a friend, with pagination.
func (f *FriendMgo) FindInWhoseFriends(ctx context.Context, friendUserID string, pagination pagination.Pagination) (int64, []*relation.FriendModel, error) {
	filter := bson.M{"friend_user_id": friendUserID}
	return mongoutil.FindPage[*relation.FriendModel](ctx, f.coll, filter, pagination)
}

// FindFriendUserIDs retrieves a list of friend user IDs for a given owner.
func (f *FriendMgo) FindFriendUserIDs(ctx context.Context, ownerUserID string) ([]string, error) {
	filter := bson.M{"owner_user_id": ownerUserID}
	return mongoutil.Find[string](ctx, f.coll, filter, options.Find().SetProjection(bson.M{"_id": 0, "friend_user_id": 1}))
}

func (f *FriendMgo) UpdateFriends(ctx context.Context, ownerUserID string, friendUserIDs []string, val map[string]any) error {
	// Ensure there are IDs to update
	if len(friendUserIDs) == 0 {
		return nil // Or return an error if you expect there to always be IDs
	}

	// Create a filter to match documents with the specified ownerUserID and any of the friendUserIDs
	filter := bson.M{
		"owner_user_id":  ownerUserID,
		"friend_user_id": bson.M{"$in": friendUserIDs},
	}

	// Create an update document
	update := bson.M{"$set": val}

	// Perform the update operation for all matching documents
	_, err := mongoutil.UpdateMany(ctx, f.coll, filter, update)
	return err
}

type Friend struct {
	Nickname string `bson:"nickname"`
	Remark   string `bson:"remark"`
}

func (f *FriendMgo) aggregatePagination(ctx context.Context, where any, pagination pagination.Pagination) (int64, []*relation.FriendModel, error) {
	if pagination == nil {
		return 0, nil, errs.ErrArgs.WrapMsg("pagination is required")
	}
	count, err := mongoutil.Count(ctx, f.coll, where)
	if err != nil {
		return 0, nil, err
	}
	skip := int64(pagination.GetPageNumber()-1) * int64(pagination.GetShowNumber())
	if skip < 0 || skip >= count || pagination.GetShowNumber() <= 0 {
		return count, nil, nil
	}
	pipeline := []bson.M{
		{
			"$match": where,
		},
		{
			"$addFields": bson.M{
				"sort_field": bson.M{
					"$cond": bson.M{
						"if":   bson.M{"$eq": bson.A{"$remark", ""}},
						"then": "$nickname",
						"else": "$remark",
					},
				},
			},
		},
		{
			"$sort": bson.D{
				{"sort_field", 1},
				{"create_time", 1},
			},
		},
		{
			"$skip": skip,
		},
		{
			"$limit": pagination.GetShowNumber(),
		},
	}
	friends, err := mongoutil.Aggregate[*relation.FriendModel](ctx, f.coll, pipeline)
	if err != nil {
		return 0, nil, err
	}
	return count, friends, nil
}

func (f *FriendMgo) SearchFriend(ctx context.Context, ownerUserID, keyword string, pagination pagination.Pagination) (int64, []*relation.FriendModel, error) {
	where := bson.M{
		"owner_user_id": ownerUserID,
		"$or": []bson.M{
			{"remark": bson.M{"$regex": keyword, "$options": "i"}},
			{"friend_user_id": bson.M{"$regex": keyword, "$options": "i"}},
			{"nickname": bson.M{"$regex": keyword, "$options": "i"}},
		},
	}
	return f.aggregatePagination(ctx, where, pagination)
}

func (f *FriendMgo) FindFriendUserID(ctx context.Context, friendUserID string) ([]string, error) {
	filter := bson.M{
		"friend_user_id": friendUserID,
	}
	return mongoutil.Find[string](ctx, f.coll, filter, options.Find().SetProjection(bson.M{"_id": 0, "owner_user_id": 1}))
}

func (f *FriendMgo) UpdateFriendUserInfo(ctx context.Context, friendUserID string, nickname string, faceURL string) error {
	filter := bson.M{
		"friend_user_id": friendUserID,
	}
	_, err := mongoutil.UpdateMany(ctx, f.coll, filter, bson.M{"$set": bson.M{"nickname": nickname, "face_url": faceURL}})
	return err
}
