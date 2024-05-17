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

package grouphash

import (
	"context"
	"crypto/md5"
	"encoding/binary"
	"encoding/json"
	"github.com/openimsdk/tools/errs"
	"strconv"
	"strings"

	"github.com/openimsdk/protocol/group"
	"github.com/openimsdk/protocol/sdkws"
	"github.com/openimsdk/tools/utils/datautil"
)

func NewGroupHashFromGroupClient(x group.GroupClient) *GroupHash {
	return &GroupHash{
		getGroupAllUserIDs: func(ctx context.Context, groupID string) ([]string, error) {
			resp, err := x.GetGroupMemberUserIDs(ctx, &group.GetGroupMemberUserIDsReq{GroupID: groupID})
			if err != nil {
				return nil, err
			}
			return resp.UserIDs, nil
		},
		getGroupMemberInfo: func(ctx context.Context, groupID string, userIDs []string) ([]*sdkws.GroupMemberFullInfo, error) {
			resp, err := x.GetGroupMembersInfo(ctx, &group.GetGroupMembersInfoReq{GroupID: groupID, UserIDs: userIDs})
			if err != nil {
				return nil, err
			}
			return resp.Members, nil
		},
	}
}

func NewGroupHashFromGroupServer(x group.GroupServer, getGroupHashPart func(ctx context.Context, groupID string) ([]string, error)) *GroupHash {
	return &GroupHash{
		getGroupAllUserIDs: func(ctx context.Context, groupID string) ([]string, error) {
			resp, err := x.GetGroupMemberUserIDs(ctx, &group.GetGroupMemberUserIDsReq{GroupID: groupID})
			if err != nil {
				return nil, err
			}
			return resp.UserIDs, nil
		},
		getGroupMemberInfo: func(ctx context.Context, groupID string, userIDs []string) ([]*sdkws.GroupMemberFullInfo, error) {
			resp, err := x.GetGroupMembersInfo(ctx, &group.GetGroupMembersInfoReq{GroupID: groupID, UserIDs: userIDs})
			if err != nil {
				return nil, err
			}
			return resp.Members, nil
		},
		getGroupHashPart: getGroupHashPart,
	}
}

type GroupHash struct {
	getGroupAllUserIDs func(ctx context.Context, groupID string) ([]string, error)
	getGroupMemberInfo func(ctx context.Context, groupID string, userIDs []string) ([]*sdkws.GroupMemberFullInfo, error)
	getGroupHashPart   func(ctx context.Context, groupID string) ([]string, error)
}

func (gh *GroupHash) GetGroupHash(ctx context.Context, groupID string) (uint64, error) {
	userIDs, err := gh.getGroupAllUserIDs(ctx, groupID)
	if err != nil {
		return 0, err
	}
	var members []*sdkws.GroupMemberFullInfo
	if len(userIDs) > 0 {
		members, err = gh.getGroupMemberInfo(ctx, groupID, userIDs)
		if err != nil {
			return 0, err
		}
		datautil.Sort(userIDs, true)
	}
	memberMap := datautil.SliceToMap(members, func(e *sdkws.GroupMemberFullInfo) string {
		return e.UserID
	})
	res := make([]*sdkws.GroupMemberFullInfo, 0, len(members))
	for _, userID := range userIDs {
		member, ok := memberMap[userID]
		if !ok {
			continue
		}
		member.AppMangerLevel = 0
		res = append(res, member)
	}
	data, err := json.Marshal(res)
	if err != nil {
		return 0, err
	}
	sum := md5.Sum(data)
	return binary.BigEndian.Uint64(sum[:]), nil
}

func (gh *GroupHash) GetGroupHashPart(ctx context.Context, groupID string) (uint64, error) {
	userIDs, err := gh.getGroupHashPart(ctx, groupID)
	if err != nil {
		return 0, err
	}
	if len(userIDs) == 0 {
		return 0, nil
	}
	members, err := gh.getGroupMemberInfo(ctx, groupID, userIDs)
	if err != nil {
		return 0, err
	}
	if len(userIDs) != len(members) {
		return 0, errs.ErrInternalServer.WrapMsg("inconsistent acquisition of group members")
	}
	memberMap := datautil.SliceToMap(members, func(m *sdkws.GroupMemberFullInfo) string {
		return m.UserID
	})
	arr := make([]string, 0, len(members))
	for _, userID := range userIDs {
		m, ok := memberMap[userID]
		if ok {
			return 0, errs.ErrInternalServer.WrapMsg("GetGroupPart member not found", "groupID", groupID, "userID", userID)
		}
		arr = append(arr, strings.Join(
			[]string{
				m.UserID,
				m.Nickname,
				m.FaceURL,
				strconv.FormatInt(int64(m.RoleLevel), 10),
				strconv.FormatInt(m.JoinTime, 10),
				strconv.FormatInt(int64(m.JoinSource), 10),
				m.InviterUserID,
				strconv.FormatInt(m.MuteEndTime, 10),
				m.OperatorUserID,
				m.Ex,
			}, ","))
	}
	hashStr := strings.Join(arr, ";")
	sum := md5.Sum([]byte(hashStr))
	return binary.BigEndian.Uint64(sum[:]), nil
}
