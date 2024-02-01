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

package unrelation

import (
	"context"
	"github.com/OpenIMSDK/tools/pagination"
)

// SubscribeUser collection constant.
const (
	SubscribeUser = "subscribe_user"
)

// UserModel collection structure.
type UserModel struct {
	UserID     string   `bson:"user_id"      json:"userID"`
	UserIDList []string `bson:"user_id_list" json:"userIDList"`
}

func (UserModel) TableName() string {
	return SubscribeUser
}

// UserModelInterface Operation interface of user mongodb.
type UserModelInterface interface {
	// AddSubscriptionList Subscriber's handling of thresholds.
	AddSubscriptionList(ctx context.Context, userID string, userIDList []string) error
	// UnsubscriptionList Handling of unsubscribe.
	UnsubscriptionList(ctx context.Context, userID string, userIDList []string) error

	// GetAllSubscribeList Get all users subscribed by this user
	GetAllSubscribeList(ctx context.Context, userID string) (userIDList []string, err error)
	// GetSubscribedList Get the user subscribed by those users
	GetSubscribedList(ctx context.Context, userID string) (userIDList []string, err error)

	SetUserOnline(ctx context.Context, userID string, connID string, platformID int32) (bool, error)

	SetUserOffline(ctx context.Context, userID string, connID string) (bool, error)

	GetUserOnline(ctx context.Context, userID string) ([]int32, error)

	SetGroupOnline(ctx context.Context, userID string, online bool, groupIDs []string) error

	GetGroupOnlineNum(ctx context.Context, groupID string) (int64, error)
	GetGroupOnlineUserIDs(ctx context.Context, groupID string) ([]string, error)
	GetGroupOnline(ctx context.Context, groupID string, desc bool, pagination pagination.Pagination) (int64, []string, error)
}
