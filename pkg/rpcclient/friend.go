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

package rpcclient

import (
	"context"

	"github.com/OpenIMSDK/protocol/friend"
	sdkws "github.com/OpenIMSDK/protocol/sdkws"
	"github.com/OpenIMSDK/tools/discoveryregistry"
	"google.golang.org/grpc"

	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	util "github.com/openimsdk/open-im-server/v3/pkg/util/genutil"
)

type Friend struct {
	conn   grpc.ClientConnInterface
	Client friend.FriendClient
	discov discoveryregistry.SvcDiscoveryRegistry
	Config *config.GlobalConfig
}

func NewFriend(discov discoveryregistry.SvcDiscoveryRegistry, config *config.GlobalConfig) *Friend {
	conn, err := discov.GetConn(context.Background(), config.RpcRegisterName.OpenImFriendName)
	if err != nil {
		util.ExitWithError(err)
	}
	client := friend.NewFriendClient(conn)
	return &Friend{discov: discov, conn: conn, Client: client, Config: config}
}

type FriendRpcClient Friend

func NewFriendRpcClient(discov discoveryregistry.SvcDiscoveryRegistry, config *config.GlobalConfig) FriendRpcClient {
	return FriendRpcClient(*NewFriend(discov, config))
}

func (f *FriendRpcClient) GetFriendsInfo(
	ctx context.Context,
	ownerUserID, friendUserID string,
) (resp *sdkws.FriendInfo, err error) {
	r, err := f.Client.GetDesignatedFriends(
		ctx,
		&friend.GetDesignatedFriendsReq{OwnerUserID: ownerUserID, FriendUserIDs: []string{friendUserID}},
	)
	if err != nil {
		return nil, err
	}
	resp = r.FriendsInfo[0]
	return
}

// possibleFriendUserID Is PossibleFriendUserId's friends.
func (f *FriendRpcClient) IsFriend(ctx context.Context, possibleFriendUserID, userID string) (bool, error) {
	resp, err := f.Client.IsFriend(ctx, &friend.IsFriendReq{UserID1: userID, UserID2: possibleFriendUserID})
	if err != nil {
		return false, err
	}
	return resp.InUser1Friends, nil
}

func (f *FriendRpcClient) GetFriendIDs(ctx context.Context, ownerUserID string) (friendIDs []string, err error) {
	req := friend.GetFriendIDsReq{UserID: ownerUserID}
	resp, err := f.Client.GetFriendIDs(ctx, &req)
	if err != nil {
		return nil, err
	}
	return resp.FriendIDs, nil
}

func (b *FriendRpcClient) IsBlack(ctx context.Context, possibleBlackUserID, userID string) (bool, error) {
	r, err := b.Client.IsBlack(ctx, &friend.IsBlackReq{UserID1: possibleBlackUserID, UserID2: userID})
	if err != nil {
		return false, err
	}
	return r.InUser2Blacks, nil
}
