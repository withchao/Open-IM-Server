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
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"

	"github.com/openimsdk/tools/db/mongoutil"
	"github.com/openimsdk/tools/db/pagination"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// FriendMgo implements Friend using MongoDB as the storage backend.
type FriendMgo struct {
	coll  *mongo.Collection
	owner database.VersionLog
}

// NewFriendMongo creates a new instance of FriendMgo with the provided MongoDB database.
func NewFriendMongo(db *mongo.Database) (database.Friend, error) {
	coll := db.Collection("friend")
	_, err := coll.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys: bson.D{
			{Key: "owner_user_id", Value: 1},
			{Key: "friend_user_id", Value: 1},
		},
		Options: options.Index().SetUnique(true),
	})
	if err != nil {
		return nil, err
	}
	owner, err := NewVersionLog(db.Collection("friend_version"))
	if err != nil {
		return nil, err
	}
	return &FriendMgo{coll: coll, owner: owner}, nil
}

func (f *FriendMgo) friendSort() any {
	return bson.D{{"is_pinned", -1}, {"friend_nickname", 1}, {"create_time", 1}}
}

// Create inserts multiple friend records.
func (f *FriendMgo) Create(ctx context.Context, friends []*model.Friend) error {
	return mongoutil.IncrVersion(func() error {
		return mongoutil.InsertMany(ctx, f.coll, friends)
	}, func() error {
		mp := make(map[string][]string)
		for _, friend := range friends {
			mp[friend.OwnerUserID] = append(mp[friend.OwnerUserID], friend.FriendUserID)
		}
		for ownerUserID, friendUserIDs := range mp {
			if err := f.owner.IncrVersion(ctx, ownerUserID, friendUserIDs, false); err != nil {
				return err
			}
		}
		return nil
	})
}

// Delete removes specified friends of the owner user.
func (f *FriendMgo) Delete(ctx context.Context, ownerUserID string, friendUserIDs []string) error {
	filter := bson.M{
		"owner_user_id":  ownerUserID,
		"friend_user_id": bson.M{"$in": friendUserIDs},
	}
	return mongoutil.IncrVersion(func() error {
		return mongoutil.DeleteOne(ctx, f.coll, filter)
	}, func() error {
		return f.owner.IncrVersion(ctx, ownerUserID, friendUserIDs, true)
	})
}

// UpdateByMap updates specific fields of a friend document using a map.
func (f *FriendMgo) UpdateByMap(ctx context.Context, ownerUserID string, friendUserID string, args map[string]any) error {
	if len(args) == 0 {
		return nil
	}
	filter := bson.M{
		"owner_user_id":  ownerUserID,
		"friend_user_id": friendUserID,
	}
	return mongoutil.IncrVersion(func() error {
		return mongoutil.UpdateOne(ctx, f.coll, filter, bson.M{"$set": args}, true)
	}, func() error {
		return f.owner.IncrVersion(ctx, ownerUserID, []string{friendUserID}, false)
	})
}

// UpdateRemark updates the remark for a specific friend.
func (f *FriendMgo) UpdateRemark(ctx context.Context, ownerUserID, friendUserID, remark string) error {
	return f.UpdateByMap(ctx, ownerUserID, friendUserID, map[string]any{"remark": remark})
}

// Take retrieves a single friend document. Returns an error if not found.
func (f *FriendMgo) Take(ctx context.Context, ownerUserID, friendUserID string) (*model.Friend, error) {
	filter := bson.M{
		"owner_user_id":  ownerUserID,
		"friend_user_id": friendUserID,
	}
	return mongoutil.FindOne[*model.Friend](ctx, f.coll, filter)
}

// FindUserState finds the friendship status between two users.
func (f *FriendMgo) FindUserState(ctx context.Context, userID1, userID2 string) ([]*model.Friend, error) {
	filter := bson.M{
		"$or": []bson.M{
			{"owner_user_id": userID1, "friend_user_id": userID2},
			{"owner_user_id": userID2, "friend_user_id": userID1},
		},
	}
	return mongoutil.Find[*model.Friend](ctx, f.coll, filter)
}

// FindFriends retrieves a list of friends for a given owner. Missing friends do not cause an error.
func (f *FriendMgo) FindFriends(ctx context.Context, ownerUserID string, friendUserIDs []string) ([]*model.Friend, error) {
	filter := bson.M{
		"owner_user_id":  ownerUserID,
		"friend_user_id": bson.M{"$in": friendUserIDs},
	}
	return mongoutil.Find[*model.Friend](ctx, f.coll, filter)
}

// FindReversalFriends finds users who have added the specified user as a friend.
func (f *FriendMgo) FindReversalFriends(ctx context.Context, friendUserID string, ownerUserIDs []string) ([]*model.Friend, error) {
	filter := bson.M{
		"owner_user_id":  bson.M{"$in": ownerUserIDs},
		"friend_user_id": friendUserID,
	}
	return mongoutil.Find[*model.Friend](ctx, f.coll, filter)
}

// FindOwnerFriends retrieves a paginated list of friends for a given owner.
func (f *FriendMgo) FindOwnerFriends(ctx context.Context, ownerUserID string, pagination pagination.Pagination) (int64, []*model.Friend, error) {
	filter := bson.M{"owner_user_id": ownerUserID}
	opt := options.Find().SetSort(f.friendSort())
	return mongoutil.FindPage[*model.Friend](ctx, f.coll, filter, pagination, opt)
}

func (f *FriendMgo) FindOwnerFriendUserIds(ctx context.Context, ownerUserID string, limit int) ([]string, error) {
	filter := bson.M{"owner_user_id": ownerUserID}
	opt := options.Find().SetProjection(bson.M{"_id": 0, "friend_user_id": 1}).SetSort(f.friendSort()).SetLimit(int64(limit))
	return mongoutil.Find[string](ctx, f.coll, filter, opt)
}

// FindInWhoseFriends finds users who have added the specified user as a friend, with pagination.
func (f *FriendMgo) FindInWhoseFriends(ctx context.Context, friendUserID string, pagination pagination.Pagination) (int64, []*model.Friend, error) {
	filter := bson.M{"friend_user_id": friendUserID}
	return mongoutil.FindPage[*model.Friend](ctx, f.coll, filter, pagination)
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

	return mongoutil.IncrVersion(func() error {
		return mongoutil.Ignore(mongoutil.UpdateMany(ctx, f.coll, filter, update))
	}, func() error {
		return f.owner.IncrVersion(ctx, ownerUserID, friendUserIDs, false)
	})
}

func (f *FriendMgo) FindIncrVersion(ctx context.Context, ownerUserID string, version uint, limit int) (*model.VersionLog, error) {
	return f.owner.FindChangeLog(ctx, ownerUserID, version, limit)
}

func (f *FriendMgo) FindFriendUserID(ctx context.Context, friendUserID string) ([]string, error) {
	filter := bson.M{
		"friend_user_id": friendUserID,
	}
	return mongoutil.Find[string](ctx, f.coll, filter, options.Find().SetProjection(bson.M{"_id": 0, "owner_user_id": 1}).SetSort(f.friendSort()))
}

func (f *FriendMgo) UpdateFriendUserInfo(ctx context.Context, friendUserID string, nickname string, faceURL string) error {
	filter := bson.M{
		"friend_user_id": friendUserID,
	}
	_, err := mongoutil.UpdateMany(ctx, f.coll, filter, bson.M{"$set": bson.M{"nickname": nickname, "face_url": faceURL}})
	return err
}

func (f *FriendMgo) SearchFriend(ctx context.Context, ownerUserID, keyword string, pagination pagination.Pagination) (int64, []*model.Friend, error) {
	filter := bson.M{
		"owner_user_id": ownerUserID,
	}
	if keyword != "" {
		filter["$or"] = []bson.M{
			{"remark": bson.M{"$regex": keyword, "$options": "i"}},
			{"nickname": bson.M{"$regex": keyword, "$options": "i"}},
			{"friend_user_id": bson.M{"$regex": keyword, "$options": "i"}},
		}
	}
	opt := options.Find().SetSort(f.friendSort())
	return mongoutil.FindPage[*model.Friend](ctx, f.coll, filter, pagination, opt)
}
