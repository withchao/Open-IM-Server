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

package group

import (
	"context"
	"fmt"
	"math/big"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"github.com/openimsdk/open-im-server/v3/pkg/authverify"
	"github.com/openimsdk/open-im-server/v3/pkg/callbackstruct"
	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/open-im-server/v3/pkg/common/convert"
	"github.com/openimsdk/open-im-server/v3/pkg/common/db/cache"
	"github.com/openimsdk/open-im-server/v3/pkg/common/db/controller"
	"github.com/openimsdk/open-im-server/v3/pkg/common/db/mgo"
	relationtb "github.com/openimsdk/open-im-server/v3/pkg/common/db/table/relation"
	"github.com/openimsdk/open-im-server/v3/pkg/common/db/unrelation"
	"github.com/openimsdk/open-im-server/v3/pkg/msgprocessor"
	"github.com/openimsdk/open-im-server/v3/pkg/rpcclient"
	"github.com/openimsdk/open-im-server/v3/pkg/rpcclient/grouphash"
	"github.com/openimsdk/open-im-server/v3/pkg/rpcclient/notification"
	"github.com/openimsdk/protocol/constant"
	pbconversation "github.com/openimsdk/protocol/conversation"
	pbgroup "github.com/openimsdk/protocol/group"
	"github.com/openimsdk/protocol/sdkws"
	"github.com/openimsdk/tools/discoveryregistry"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/mcontext"
	"github.com/openimsdk/tools/mw/specialerror"
	"github.com/openimsdk/tools/tx"
	"github.com/openimsdk/tools/utils"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

type groupServer struct {
	db                    controller.GroupDatabase
	User                  rpcclient.UserRpcClient
	Notification          *notification.GroupNotificationSender
	conversationRpcClient rpcclient.ConversationRpcClient
	msgRpcClient          rpcclient.MessageRpcClient
	config                *config.GlobalConfig
}

func Start(ctx context.Context, config *config.GlobalConfig, client discoveryregistry.SvcDiscoveryRegistry, server *grpc.Server) error {
	mongo, err := unrelation.NewMongoDB(ctx, &config.Mongo)
	if err != nil {
		return err
	}
	rdb, err := cache.NewRedis(ctx, &config.Redis)
	if err != nil {
		return err
	}

	groupDB, err := mgo.NewGroupMongo(mongo.GetDatabase(config.Mongo.Database))
	if err != nil {
		return err
	}

	groupMemberDB, err := mgo.NewGroupMember(mongo.GetDatabase(config.Mongo.Database))
	if err != nil {
		return err
	}

	groupRequestDB, err := mgo.NewGroupRequestMgo(mongo.GetDatabase(config.Mongo.Database))
	if err != nil {
		return err
	}

	userRpcClient := rpcclient.NewUserRpcClient(client, config.RpcRegisterName.OpenImUserName, &config.Manager, &config.IMAdmin)
	msgRpcClient := rpcclient.NewMessageRpcClient(client, config.RpcRegisterName.OpenImMsgName)
	conversationRpcClient := rpcclient.NewConversationRpcClient(client, config.RpcRegisterName.OpenImConversationName)
	var gs groupServer
	database := controller.NewGroupDatabase(rdb, groupDB, groupMemberDB, groupRequestDB, tx.NewMongo(mongo.GetClient()), grouphash.NewGroupHashFromGroupServer(&gs))
	gs.db = database
	gs.User = userRpcClient
	gs.Notification = notification.NewGroupNotificationSender(database, &msgRpcClient, &userRpcClient, config, func(ctx context.Context, userIDs []string) ([]notification.CommonUser, error) {
		users, err := userRpcClient.GetUsersInfo(ctx, userIDs)
		if err != nil {
			return nil, err
		}
		return utils.Slice(users, func(e *sdkws.UserInfo) notification.CommonUser { return e }), nil
	})
	gs.conversationRpcClient = conversationRpcClient
	gs.msgRpcClient = msgRpcClient
	gs.config = config
	pbgroup.RegisterGroupServer(server, &gs)
	return nil
}

func (s *groupServer) GetJoinedGroupIDs(ctx context.Context, req *pbgroup.GetJoinedGroupIDsReq) (*pbgroup.GetJoinedGroupIDsResp, error) {
	// TODO implement me
	panic("implement me")
}

func (s *groupServer) NotificationUserInfoUpdate(ctx context.Context, req *pbgroup.NotificationUserInfoUpdateReq) (*pbgroup.NotificationUserInfoUpdateResp, error) {
	members, err := s.db.FindGroupMemberUser(ctx, nil, req.UserID)
	if err != nil {
		return nil, err
	}
	groupIDs := make([]string, 0, len(members))
	for _, member := range members {
		if member.Nickname != "" && member.FaceURL != "" {
			continue
		}
		groupIDs = append(groupIDs, member.GroupID)
	}
	for _, groupID := range groupIDs {
		if err = s.Notification.GroupMemberInfoSetNotification(ctx, groupID, req.UserID); err != nil {
			return nil, err
		}
	}
	if err = s.db.DeleteGroupMemberHash(ctx, groupIDs); err != nil {
		return nil, err
	}

	return &pbgroup.NotificationUserInfoUpdateResp{}, nil
}

func (s *groupServer) CheckGroupAdmin(ctx context.Context, groupID string) error {
	if !authverify.IsAppManagerUid(ctx, &s.config.Manager, &s.config.IMAdmin) {
		groupMember, err := s.db.TakeGroupMember(ctx, groupID, mcontext.GetOpUserID(ctx))
		if err != nil {
			return err
		}
		if !(groupMember.RoleLevel == constant.GroupOwner || groupMember.RoleLevel == constant.GroupAdmin) {
			return errs.ErrNoPermission.WrapMsg("no group owner or admin")
		}
	}
	return nil
}

func (s *groupServer) GetPublicUserInfoMap(ctx context.Context, userIDs []string, complete bool) (map[string]*sdkws.PublicUserInfo, error) {
	if len(userIDs) == 0 {
		return map[string]*sdkws.PublicUserInfo{}, nil
	}
	users, err := s.User.GetPublicUserInfos(ctx, userIDs, complete)
	if err != nil {
		return nil, err
	}
	return utils.SliceToMapAny(users, func(e *sdkws.PublicUserInfo) (string, *sdkws.PublicUserInfo) {
		return e.UserID, e
	}), nil
}

func (s *groupServer) IsNotFound(err error) bool {
	return errs.ErrRecordNotFound.Is(specialerror.ErrCode(errs.Unwrap(err)))
}

func (s *groupServer) GenGroupID(ctx context.Context, groupID *string) error {
	if *groupID != "" {
		_, err := s.db.TakeGroup(ctx, *groupID)
		if err == nil {
			return errs.ErrGroupIDExisted.WrapMsg("group id existed " + *groupID)
		} else if s.IsNotFound(err) {
			return nil
		} else {
			return err
		}
	}
	for i := 0; i < 10; i++ {
		id := utils.Md5(strings.Join([]string{mcontext.GetOperationID(ctx), strconv.FormatInt(time.Now().UnixNano(), 10), strconv.Itoa(rand.Int())}, ",;,"))
		bi := big.NewInt(0)
		bi.SetString(id[0:8], 16)
		id = bi.String()
		_, err := s.db.TakeGroup(ctx, id)
		if err == nil {
			continue
		} else if s.IsNotFound(err) {
			*groupID = id
			return nil
		} else {
			return err
		}
	}
	return errs.ErrData.WrapMsg("group id gen error")
}

func (s *groupServer) CreateGroup(ctx context.Context, req *pbgroup.CreateGroupReq) (*pbgroup.CreateGroupResp, error) {
	if req.GroupInfo.GroupType != constant.WorkingGroup {
		return nil, errs.ErrArgs.WrapMsg(fmt.Sprintf("group type only supports %d", constant.WorkingGroup))
	}
	if req.OwnerUserID == "" {
		return nil, errs.ErrArgs.WrapMsg("no group owner")
	}
	if err := authverify.CheckAccessV3(ctx, req.OwnerUserID, &s.config.Manager, &s.config.IMAdmin); err != nil {
		return nil, err
	}
	userIDs := append(append(req.MemberUserIDs, req.AdminUserIDs...), req.OwnerUserID)
	opUserID := mcontext.GetOpUserID(ctx)
	if !utils.Contain(opUserID, userIDs...) {
		userIDs = append(userIDs, opUserID)
	}

	if utils.Duplicate(userIDs) {
		return nil, errs.ErrArgs.WrapMsg("group member repeated")
	}

	userMap, err := s.User.GetUsersInfoMap(ctx, userIDs)
	if err != nil {
		return nil, err
	}

	if len(userMap) != len(userIDs) {
		return nil, errs.ErrUserIDNotFound.WrapMsg("user not found")
	}

	config := &GroupEventCallbackConfig{
		CallbackUrl:       s.config.Callback.CallbackUrl,
		BeforeCreateGroup: s.config.Callback.CallbackBeforeCreateGroup,
	}

	// Callback Before create Group
	if err := CallbackBeforeCreateGroup(ctx, config, req); err != nil {
		return nil, err
	}

	var groupMembers []*relationtb.GroupMemberModel
	group := convert.Pb2DBGroupInfo(req.GroupInfo)
	if err := s.GenGroupID(ctx, &group.GroupID); err != nil {
		return nil, err
	}

	beforeCreateGroupConfig := &GroupEventCallbackConfig{
		CallbackUrl:       s.config.Callback.CallbackUrl,
		BeforeCreateGroup: s.config.Callback.CallbackBeforeMemberJoinGroup,
	}

	joinGroup := func(userID string, roleLevel int32) error {
		groupMember := &relationtb.GroupMemberModel{
			GroupID:        group.GroupID,
			UserID:         userID,
			RoleLevel:      roleLevel,
			OperatorUserID: opUserID,
			JoinSource:     constant.JoinByInvitation,
			InviterUserID:  opUserID,
			JoinTime:       time.Now(),
			MuteEndTime:    time.UnixMilli(0),
		}

		if err := CallbackBeforeMemberJoinGroup(ctx, beforeCreateGroupConfig, groupMember, group.Ex); err != nil {
			return err
		}
		groupMembers = append(groupMembers, groupMember)
		return nil
	}
	if err := joinGroup(req.OwnerUserID, constant.GroupOwner); err != nil {
		return nil, err
	}
	for _, userID := range req.AdminUserIDs {
		if err := joinGroup(userID, constant.GroupAdmin); err != nil {
			return nil, err
		}
	}
	for _, userID := range req.MemberUserIDs {
		if err := joinGroup(userID, constant.GroupOrdinaryUsers); err != nil {
			return nil, err
		}
	}
	if err := s.db.CreateGroup(ctx, []*relationtb.GroupModel{group}, groupMembers); err != nil {
		return nil, err
	}
	resp := &pbgroup.CreateGroupResp{GroupInfo: &sdkws.GroupInfo{}}

	resp.GroupInfo = convert.Db2PbGroupInfo(group, req.OwnerUserID, uint32(len(userIDs)))
	resp.GroupInfo.MemberCount = uint32(len(userIDs))
	tips := &sdkws.GroupCreatedTips{
		Group:          resp.GroupInfo,
		OperationTime:  group.CreateTime.UnixMilli(),
		GroupOwnerUser: s.groupMemberDB2PB(groupMembers[0], userMap[groupMembers[0].UserID].AppMangerLevel),
	}
	for _, member := range groupMembers {
		member.Nickname = userMap[member.UserID].Nickname
		tips.MemberList = append(tips.MemberList, s.groupMemberDB2PB(member, userMap[member.UserID].AppMangerLevel))
		if member.UserID == opUserID {
			tips.OpUser = s.groupMemberDB2PB(member, userMap[member.UserID].AppMangerLevel)
			break
		}
	}
	if req.GroupInfo.GroupType == constant.SuperGroup {
		go func() {
			for _, userID := range userIDs {
				s.Notification.SuperGroupNotification(ctx, userID, userID)
			}
		}()
	} else {
		// s.Notification.GroupCreatedNotification(ctx, group, groupMembers, userMap)
		tips := &sdkws.GroupCreatedTips{
			Group:          resp.GroupInfo,
			OperationTime:  group.CreateTime.UnixMilli(),
			GroupOwnerUser: s.groupMemberDB2PB(groupMembers[0], userMap[groupMembers[0].UserID].AppMangerLevel),
		}
		for _, member := range groupMembers {
			member.Nickname = userMap[member.UserID].Nickname
			tips.MemberList = append(tips.MemberList, s.groupMemberDB2PB(member, userMap[member.UserID].AppMangerLevel))
			if member.UserID == opUserID {
				tips.OpUser = s.groupMemberDB2PB(member, userMap[member.UserID].AppMangerLevel)
				break
			}
		}
		s.Notification.GroupCreatedNotification(ctx, tips)
	}

	afterCreateGroupConfig := beforeCreateGroupConfig

	reqCallBackAfter := &pbgroup.CreateGroupReq{
		MemberUserIDs: userIDs,
		GroupInfo:     resp.GroupInfo,
		OwnerUserID:   req.OwnerUserID,
		AdminUserIDs:  req.AdminUserIDs,
	}

	if err := CallbackAfterCreateGroup(ctx, afterCreateGroupConfig, reqCallBackAfter); err != nil {
		return nil, err
	}

	return resp, nil
}

func (s *groupServer) GetJoinedGroupList(ctx context.Context, req *pbgroup.GetJoinedGroupListReq) (*pbgroup.GetJoinedGroupListResp, error) {
	resp := &pbgroup.GetJoinedGroupListResp{}
	if err := authverify.CheckAccessV3(ctx, req.FromUserID, &s.config.Manager, &s.config.IMAdmin); err != nil {
		return nil, err
	}
	total, members, err := s.db.PageGetJoinGroup(ctx, req.FromUserID, req.Pagination)
	if err != nil {
		return nil, err
	}
	resp.Total = uint32(total)
	if len(members) == 0 {
		return resp, nil
	}
	groupIDs := utils.Slice(members, func(e *relationtb.GroupMemberModel) string {
		return e.GroupID
	})
	groups, err := s.db.FindGroup(ctx, groupIDs)
	if err != nil {
		return nil, err
	}
	groupMemberNum, err := s.db.MapGroupMemberNum(ctx, groupIDs)
	if err != nil {
		return nil, err
	}
	owners, err := s.db.FindGroupsOwner(ctx, groupIDs)
	if err != nil {
		return nil, err
	}
	if err := s.PopulateGroupMember(ctx, members...); err != nil {
		return nil, err
	}
	ownerMap := utils.SliceToMap(owners, func(e *relationtb.GroupMemberModel) string {
		return e.GroupID
	})
	resp.Groups = utils.Slice(utils.Order(groupIDs, groups, func(group *relationtb.GroupModel) string {
		return group.GroupID
	}), func(group *relationtb.GroupModel) *sdkws.GroupInfo {
		var userID string
		if user := ownerMap[group.GroupID]; user != nil {
			userID = user.UserID
		}
		return convert.Db2PbGroupInfo(group, userID, groupMemberNum[group.GroupID])
	})
	return resp, nil
}

func (s *groupServer) InviteUserToGroup(ctx context.Context, req *pbgroup.InviteUserToGroupReq) (*pbgroup.InviteUserToGroupResp, error) {
	resp := &pbgroup.InviteUserToGroupResp{}

	if len(req.InvitedUserIDs) == 0 {
		return nil, errs.ErrArgs.WrapMsg("user empty")
	}
	if utils.Duplicate(req.InvitedUserIDs) {
		return nil, errs.ErrArgs.WrapMsg("userID duplicate")
	}
	group, err := s.db.TakeGroup(ctx, req.GroupID)
	if err != nil {
		return nil, err
	}

	if group.Status == constant.GroupStatusDismissed {
		return nil, errs.ErrDismissedAlready.WrapMsg("group dismissed checking group status found it dismissed")
	}

	userMap, err := s.User.GetUsersInfoMap(ctx, req.InvitedUserIDs)
	if err != nil {
		return nil, err
	}

	if len(userMap) != len(req.InvitedUserIDs) {
		return nil, errs.ErrRecordNotFound.WrapMsg("user not found")
	}

	var groupMember *relationtb.GroupMemberModel
	var opUserID string
	if !authverify.IsAppManagerUid(ctx, &s.config.Manager, &s.config.IMAdmin) {
		opUserID = mcontext.GetOpUserID(ctx)
		var err error
		groupMember, err = s.db.TakeGroupMember(ctx, req.GroupID, opUserID)
		if err != nil {
			return nil, err
		}
		if err := s.PopulateGroupMember(ctx, groupMember); err != nil {
			return nil, err
		}
	}

	beforeInviteUserToGroupConfig := &GroupEventCallbackConfig{
		CallbackUrl:       s.config.Callback.CallbackUrl,
		BeforeCreateGroup: s.config.Callback.CallbackBeforeInviteUserToGroup,
	}

	if err := CallbackBeforeInviteUserToGroup(ctx, beforeInviteUserToGroupConfig, req); err != nil {
		return nil, err
	}

	if group.NeedVerification == constant.AllNeedVerification {
		if !authverify.IsAppManagerUid(ctx, &s.config.Manager, &s.config.IMAdmin) {
			if !(groupMember.RoleLevel == constant.GroupOwner || groupMember.RoleLevel == constant.GroupAdmin) {
				var requests []*relationtb.GroupRequestModel
				for _, userID := range req.InvitedUserIDs {
					requests = append(requests, &relationtb.GroupRequestModel{
						UserID:        userID,
						GroupID:       req.GroupID,
						JoinSource:    constant.JoinByInvitation,
						InviterUserID: opUserID,
						ReqTime:       time.Now(),
						HandledTime:   time.Unix(0, 0),
					})
				}
				if err := s.db.CreateGroupRequest(ctx, requests); err != nil {
					return nil, err
				}
				for _, request := range requests {
					s.Notification.JoinGroupApplicationNotification(ctx, &pbgroup.JoinGroupReq{
						GroupID:       request.GroupID,
						ReqMessage:    request.ReqMsg,
						JoinSource:    request.JoinSource,
						InviterUserID: request.InviterUserID,
					})
				}
				return resp, nil
			}
		}
	}
	var groupMembers []*relationtb.GroupMemberModel
	for _, userID := range req.InvitedUserIDs {
		member := &relationtb.GroupMemberModel{
			GroupID:        req.GroupID,
			UserID:         userID,
			RoleLevel:      constant.GroupOrdinaryUsers,
			OperatorUserID: opUserID,
			InviterUserID:  opUserID,
			JoinSource:     constant.JoinByInvitation,
			JoinTime:       time.Now(),
			MuteEndTime:    time.UnixMilli(0),
		}

		beforeMemberJoinGroupConfig := beforeInviteUserToGroupConfig

		if err := CallbackBeforeMemberJoinGroup(ctx, beforeMemberJoinGroupConfig, member, group.Ex); err != nil {
			return nil, err
		}
		groupMembers = append(groupMembers, member)
	}
	if err := s.db.CreateGroup(ctx, nil, groupMembers); err != nil {
		return nil, err
	}
	if err := s.conversationRpcClient.GroupChatFirstCreateConversation(ctx, req.GroupID, req.InvitedUserIDs); err != nil {
		return nil, err
	}
	s.Notification.MemberInvitedNotification(ctx, req.GroupID, req.Reason, req.InvitedUserIDs)
	return resp, nil
}

func (s *groupServer) GetGroupAllMember(ctx context.Context, req *pbgroup.GetGroupAllMemberReq) (*pbgroup.GetGroupAllMemberResp, error) {
	members, err := s.db.FindGroupMemberAll(ctx, req.GroupID)
	if err != nil {
		return nil, err
	}
	if err := s.PopulateGroupMember(ctx, members...); err != nil {
		return nil, err
	}
	resp := &pbgroup.GetGroupAllMemberResp{}
	resp.Members = utils.Slice(members, func(e *relationtb.GroupMemberModel) *sdkws.GroupMemberFullInfo {
		return convert.Db2PbGroupMember(e)
	})
	return resp, nil
}

func (s *groupServer) GetGroupMemberList(ctx context.Context, req *pbgroup.GetGroupMemberListReq) (*pbgroup.GetGroupMemberListResp, error) {
	resp := &pbgroup.GetGroupMemberListResp{}
	var (
		total   int64
		members []*relationtb.GroupMemberModel
		err     error
	)
	if req.Keyword == "" {
		total, members, err = s.db.PageGetGroupMember(ctx, req.GroupID, req.Pagination)
	} else {
		members, err = s.db.FindGroupMemberAll(ctx, req.GroupID)
	}
	if err != nil {
		return nil, err
	}
	if err := s.PopulateGroupMember(ctx, members...); err != nil {
		return nil, err
	}
	if req.Keyword != "" {
		groupMembers := make([]*relationtb.GroupMemberModel, 0)
		for _, member := range members {
			if member.UserID == req.Keyword {
				groupMembers = append(groupMembers, member)
				total++
				continue
			}
			if member.Nickname == req.Keyword {
				groupMembers = append(groupMembers, member)
				total++
				continue
			}
		}

		GMembers := utils.Paginate(groupMembers, int(req.Pagination.GetPageNumber()), int(req.Pagination.GetShowNumber()))
		resp.Members = utils.Batch(convert.Db2PbGroupMember, GMembers)
		resp.Total = uint32(total)
		return resp, nil
	}
	resp.Total = uint32(total)
	resp.Members = utils.Batch(convert.Db2PbGroupMember, members)
	return resp, nil
}

func (s *groupServer) KickGroupMember(ctx context.Context, req *pbgroup.KickGroupMemberReq) (*pbgroup.KickGroupMemberResp, error) {
	resp := &pbgroup.KickGroupMemberResp{}
	group, err := s.db.TakeGroup(ctx, req.GroupID)
	if err != nil {
		return nil, err
	}
	if len(req.KickedUserIDs) == 0 {
		return nil, errs.ErrArgs.WrapMsg("KickedUserIDs empty")
	}
	if utils.IsDuplicateStringSlice(req.KickedUserIDs) {
		return nil, errs.ErrArgs.WrapMsg("KickedUserIDs duplicate")
	}
	opUserID := mcontext.GetOpUserID(ctx)
	if utils.IsContain(opUserID, req.KickedUserIDs) {
		return nil, errs.ErrArgs.WrapMsg("opUserID in KickedUserIDs")
	}
	members, err := s.db.FindGroupMembers(ctx, req.GroupID, append(req.KickedUserIDs, opUserID))
	if err != nil {
		return nil, err
	}
	if err := s.PopulateGroupMember(ctx, members...); err != nil {
		return nil, err
	}
	memberMap := make(map[string]*relationtb.GroupMemberModel)
	for i, member := range members {
		memberMap[member.UserID] = members[i]
	}
	isAppManagerUid := authverify.IsAppManagerUid(ctx, &s.config.Manager, &s.config.IMAdmin)
	opMember := memberMap[opUserID]
	for _, userID := range req.KickedUserIDs {
		member, ok := memberMap[userID]
		if !ok {
			return nil, errs.ErrUserIDNotFound.WrapMsg(userID)
		}
		if !isAppManagerUid {
			if opMember == nil {
				return nil, errs.ErrNoPermission.WrapMsg("opUserID no in group")
			}
			switch opMember.RoleLevel {
			case constant.GroupOwner:
			case constant.GroupAdmin:
				if member.RoleLevel == constant.GroupOwner || member.RoleLevel == constant.GroupAdmin {
					return nil, errs.ErrNoPermission.WrapMsg("group admins cannot remove the group owner and other admins")
				}
			case constant.GroupOrdinaryUsers:
				return nil, errs.ErrNoPermission.WrapMsg("opUserID no permission")
			default:
				return nil, errs.ErrNoPermission.WrapMsg("opUserID roleLevel unknown")
			}
		}
	}
	num, err := s.db.FindGroupMemberNum(ctx, req.GroupID)
	if err != nil {
		return nil, err
	}
	ownerUserIDs, err := s.db.GetGroupRoleLevelMemberIDs(ctx, req.GroupID, constant.GroupOwner)
	if err != nil {
		return nil, err
	}
	var ownerUserID string
	if len(ownerUserIDs) > 0 {
		ownerUserID = ownerUserIDs[0]
	}
	if err := s.db.DeleteGroupMember(ctx, group.GroupID, req.KickedUserIDs); err != nil {
		return nil, err
	}
	tips := &sdkws.MemberKickedTips{
		Group: &sdkws.GroupInfo{
			GroupID:                group.GroupID,
			GroupName:              group.GroupName,
			Notification:           group.Notification,
			Introduction:           group.Introduction,
			FaceURL:                group.FaceURL,
			OwnerUserID:            ownerUserID,
			CreateTime:             group.CreateTime.UnixMilli(),
			MemberCount:            num,
			Ex:                     group.Ex,
			Status:                 group.Status,
			CreatorUserID:          group.CreatorUserID,
			GroupType:              group.GroupType,
			NeedVerification:       group.NeedVerification,
			LookMemberInfo:         group.LookMemberInfo,
			ApplyMemberFriend:      group.ApplyMemberFriend,
			NotificationUpdateTime: group.NotificationUpdateTime.UnixMilli(),
			NotificationUserID:     group.NotificationUserID,
		},
		KickedUserList: []*sdkws.GroupMemberFullInfo{},
	}
	if opMember, ok := memberMap[opUserID]; ok {
		tips.OpUser = convert.Db2PbGroupMember(opMember)
	}
	for _, userID := range req.KickedUserIDs {
		tips.KickedUserList = append(tips.KickedUserList, convert.Db2PbGroupMember(memberMap[userID]))
	}
	s.Notification.MemberKickedNotification(ctx, tips)
	if err := s.deleteMemberAndSetConversationSeq(ctx, req.GroupID, req.KickedUserIDs); err != nil {
		return nil, err
	}

	killGroupMemberConfig := &GroupEventCallbackConfig{
		CallbackUrl:       s.config.Callback.CallbackUrl,
		BeforeCreateGroup: s.config.Callback.CallbackBeforeMemberJoinGroup,
	}

	if err := CallbackKillGroupMember(ctx, killGroupMemberConfig, req); err != nil {
		return nil, err
	}
	return resp, nil
}

func (s *groupServer) GetGroupMembersInfo(ctx context.Context, req *pbgroup.GetGroupMembersInfoReq) (*pbgroup.GetGroupMembersInfoResp, error) {
	resp := &pbgroup.GetGroupMembersInfoResp{}
	if len(req.UserIDs) == 0 {
		return nil, errs.ErrArgs.WrapMsg("userIDs empty")
	}
	if req.GroupID == "" {
		return nil, errs.ErrArgs.WrapMsg("groupID empty")
	}
	members, err := s.db.FindGroupMembers(ctx, req.GroupID, req.UserIDs)
	if err != nil {
		return nil, err
	}
	if err := s.PopulateGroupMember(ctx, members...); err != nil {
		return nil, err
	}
	resp.Members = utils.Slice(members, func(e *relationtb.GroupMemberModel) *sdkws.GroupMemberFullInfo {
		return convert.Db2PbGroupMember(e)
	})
	return resp, nil
}

// GetGroupApplicationList handles functions that get a list of group requests.
func (s *groupServer) GetGroupApplicationList(ctx context.Context, req *pbgroup.GetGroupApplicationListReq) (*pbgroup.GetGroupApplicationListResp, error) {
	groupIDs, err := s.db.FindUserManagedGroupID(ctx, req.FromUserID)
	if err != nil {
		return nil, err
	}
	resp := &pbgroup.GetGroupApplicationListResp{}
	if len(groupIDs) == 0 {
		return resp, nil
	}
	total, groupRequests, err := s.db.PageGroupRequest(ctx, groupIDs, req.Pagination)
	if err != nil {
		return nil, err
	}
	resp.Total = uint32(total)
	if len(groupRequests) == 0 {
		return resp, nil
	}
	var userIDs []string

	for _, gr := range groupRequests {
		userIDs = append(userIDs, gr.UserID)
	}
	userIDs = utils.Distinct(userIDs)
	userMap, err := s.User.GetPublicUserInfoMap(ctx, userIDs, true)
	if err != nil {
		return nil, err
	}
	groups, err := s.db.FindGroup(ctx, utils.Distinct(groupIDs))
	if err != nil {
		return nil, err
	}
	groupMap := utils.SliceToMap(groups, func(e *relationtb.GroupModel) string {
		return e.GroupID
	})
	if ids := utils.Single(utils.Keys(groupMap), groupIDs); len(ids) > 0 {
		return nil, errs.ErrGroupIDNotFound.WrapMsg(strings.Join(ids, ","))
	}
	groupMemberNumMap, err := s.db.MapGroupMemberNum(ctx, groupIDs)
	if err != nil {
		return nil, err
	}
	owners, err := s.db.FindGroupsOwner(ctx, groupIDs)
	if err != nil {
		return nil, err
	}
	if err := s.PopulateGroupMember(ctx, owners...); err != nil {
		return nil, err
	}
	ownerMap := utils.SliceToMap(owners, func(e *relationtb.GroupMemberModel) string {
		return e.GroupID
	})
	resp.GroupRequests = utils.Slice(groupRequests, func(e *relationtb.GroupRequestModel) *sdkws.GroupRequest {
		var ownerUserID string
		if owner, ok := ownerMap[e.GroupID]; ok {
			ownerUserID = owner.UserID
		}
		return convert.Db2PbGroupRequest(e, userMap[e.UserID], convert.Db2PbGroupInfo(groupMap[e.GroupID], ownerUserID, groupMemberNumMap[e.GroupID]))
	})
	return resp, nil
}

func (s *groupServer) GetGroupsInfo(ctx context.Context, req *pbgroup.GetGroupsInfoReq) (*pbgroup.GetGroupsInfoResp, error) {
	resp := &pbgroup.GetGroupsInfoResp{}
	if len(req.GroupIDs) == 0 {
		return nil, errs.ErrArgs.WrapMsg("groupID is empty")
	}
	groups, err := s.db.FindGroup(ctx, req.GroupIDs)
	if err != nil {
		return nil, err
	}
	groupMemberNumMap, err := s.db.MapGroupMemberNum(ctx, req.GroupIDs)
	if err != nil {
		return nil, err
	}
	owners, err := s.db.FindGroupsOwner(ctx, req.GroupIDs)
	if err != nil {
		return nil, err
	}
	if err := s.PopulateGroupMember(ctx, owners...); err != nil {
		return nil, err
	}
	ownerMap := utils.SliceToMap(owners, func(e *relationtb.GroupMemberModel) string {
		return e.GroupID
	})
	resp.GroupInfos = utils.Slice(groups, func(e *relationtb.GroupModel) *sdkws.GroupInfo {
		var ownerUserID string
		if owner, ok := ownerMap[e.GroupID]; ok {
			ownerUserID = owner.UserID
		}
		return convert.Db2PbGroupInfo(e, ownerUserID, groupMemberNumMap[e.GroupID])
	})
	return resp, nil
}

func (s *groupServer) GroupApplicationResponse(ctx context.Context, req *pbgroup.GroupApplicationResponseReq) (*pbgroup.GroupApplicationResponseResp, error) {
	if !utils.Contain(req.HandleResult, constant.GroupResponseAgree, constant.GroupResponseRefuse) {
		return nil, errs.ErrArgs.WrapMsg("HandleResult unknown")
	}
	if !authverify.IsAppManagerUid(ctx, &s.config.Manager, &s.config.IMAdmin) {
		groupMember, err := s.db.TakeGroupMember(ctx, req.GroupID, mcontext.GetOpUserID(ctx))
		if err != nil {
			return nil, err
		}
		if !(groupMember.RoleLevel == constant.GroupOwner || groupMember.RoleLevel == constant.GroupAdmin) {
			return nil, errs.ErrNoPermission.WrapMsg("no group owner or admin")
		}
	}
	group, err := s.db.TakeGroup(ctx, req.GroupID)
	if err != nil {
		return nil, err
	}
	groupRequest, err := s.db.TakeGroupRequest(ctx, req.GroupID, req.FromUserID)
	if err != nil {
		return nil, err
	}
	if groupRequest.HandleResult != 0 {
		return nil, errs.ErrGroupRequestHandled.WrapMsg("group request already processed")
	}
	var inGroup bool
	if _, err := s.db.TakeGroupMember(ctx, req.GroupID, req.FromUserID); err == nil {
		inGroup = true // Already in group
	} else if !s.IsNotFound(err) {
		return nil, err
	}
	if _, err := s.User.GetPublicUserInfo(ctx, req.FromUserID); err != nil {
		return nil, err
	}
	var member *relationtb.GroupMemberModel
	if (!inGroup) && req.HandleResult == constant.GroupResponseAgree {
		member = &relationtb.GroupMemberModel{
			GroupID:        req.GroupID,
			UserID:         req.FromUserID,
			Nickname:       "",
			FaceURL:        "",
			RoleLevel:      constant.GroupOrdinaryUsers,
			JoinTime:       time.Now(),
			JoinSource:     groupRequest.JoinSource,
			MuteEndTime:    time.Unix(0, 0),
			InviterUserID:  groupRequest.InviterUserID,
			OperatorUserID: mcontext.GetOpUserID(ctx),
			Ex:             groupRequest.Ex,
		}

		beforeMemberJoinGroupConfig := &GroupEventCallbackConfig{
			CallbackUrl:       s.config.Callback.CallbackUrl,
			BeforeCreateGroup: s.config.Callback.CallbackBeforeMemberJoinGroup,
		}

		if err = CallbackBeforeMemberJoinGroup(ctx, beforeMemberJoinGroupConfig, member, group.Ex); err != nil {
			return nil, err
		}
	}
	log.ZDebug(ctx, "GroupApplicationResponse", "inGroup", inGroup, "HandleResult", req.HandleResult, "member", member)
	if err := s.db.HandlerGroupRequest(ctx, req.GroupID, req.FromUserID, req.HandledMsg, req.HandleResult, member); err != nil {
		return nil, err
	}
	switch req.HandleResult {
	case constant.GroupResponseAgree:
		if err := s.conversationRpcClient.GroupChatFirstCreateConversation(ctx, req.GroupID, []string{req.FromUserID}); err != nil {
			return nil, err
		}
		s.Notification.GroupApplicationAcceptedNotification(ctx, req)
		if member == nil {
			log.ZDebug(ctx, "GroupApplicationResponse", "member is nil")
		} else {
			s.Notification.MemberEnterNotification(ctx, req.GroupID, req.FromUserID)
		}
	case constant.GroupResponseRefuse:
		s.Notification.GroupApplicationRejectedNotification(ctx, req)
	}

	return &pbgroup.GroupApplicationResponseResp{}, nil
}

func (s *groupServer) JoinGroup(ctx context.Context, req *pbgroup.JoinGroupReq) (resp *pbgroup.JoinGroupResp, err error) {
	user, err := s.User.GetUserInfo(ctx, req.InviterUserID)
	if err != nil {
		return nil, err
	}
	group, err := s.db.TakeGroup(ctx, req.GroupID)
	if err != nil {
		return nil, err
	}
	if group.Status == constant.GroupStatusDismissed {
		return nil, errs.ErrDismissedAlready.Wrap()
	}

	reqCall := &callbackstruct.CallbackJoinGroupReq{
		GroupID:    req.GroupID,
		GroupType:  string(group.GroupType),
		ApplyID:    req.InviterUserID,
		ReqMessage: req.ReqMessage,
		Ex:         req.Ex,
	}

	applyJoinGroupBeforeConfig := &GroupEventCallbackConfig{
		CallbackUrl:       s.config.Callback.CallbackUrl,
		BeforeCreateGroup: s.config.Callback.CallbackBeforeMemberJoinGroup,
	}

	if err = CallbackApplyJoinGroupBefore(ctx, applyJoinGroupBeforeConfig, reqCall); err != nil {
		return nil, err
	}
	_, err = s.db.TakeGroupMember(ctx, req.GroupID, req.InviterUserID)
	if err == nil {
		return nil, errs.ErrArgs.Wrap()
	} else if !s.IsNotFound(err) && utils.Unwrap(err) != errs.ErrRecordNotFound {
		return nil, err
	}
	log.ZInfo(ctx, "JoinGroup.groupInfo", "group", group, "eq", group.NeedVerification == constant.Directly)
	resp = &pbgroup.JoinGroupResp{}
	if group.NeedVerification == constant.Directly {
		groupMember := &relationtb.GroupMemberModel{
			GroupID:        group.GroupID,
			UserID:         user.UserID,
			RoleLevel:      constant.GroupOrdinaryUsers,
			OperatorUserID: mcontext.GetOpUserID(ctx),
			InviterUserID:  req.InviterUserID,
			JoinTime:       time.Now(),
			MuteEndTime:    time.UnixMilli(0),
		}
		MemberJoinGroupConfig := applyJoinGroupBeforeConfig
		if err := CallbackBeforeMemberJoinGroup(ctx, MemberJoinGroupConfig, groupMember, group.Ex); err != nil {
			return nil, err
		}
		if err := s.db.CreateGroup(ctx, nil, []*relationtb.GroupMemberModel{groupMember}); err != nil {
			return nil, err
		}

		if err := s.conversationRpcClient.GroupChatFirstCreateConversation(ctx, req.GroupID, []string{req.InviterUserID}); err != nil {
			return nil, err
		}
		s.Notification.MemberEnterNotification(ctx, req.GroupID, req.InviterUserID)
		afterJoinGroupConfig := applyJoinGroupBeforeConfig
		if err = CallbackAfterJoinGroup(ctx, afterJoinGroupConfig, req); err != nil {
			return nil, err
		}
		return resp, nil
	}
	groupRequest := relationtb.GroupRequestModel{
		UserID:      req.InviterUserID,
		ReqMsg:      req.ReqMessage,
		GroupID:     req.GroupID,
		JoinSource:  req.JoinSource,
		ReqTime:     time.Now(),
		HandledTime: time.Unix(0, 0),
		Ex:          req.Ex,
	}
	if err = s.db.CreateGroupRequest(ctx, []*relationtb.GroupRequestModel{&groupRequest}); err != nil {
		return nil, err
	}
	s.Notification.JoinGroupApplicationNotification(ctx, req)
	return resp, nil
}

func (s *groupServer) QuitGroup(ctx context.Context, req *pbgroup.QuitGroupReq) (*pbgroup.QuitGroupResp, error) {
	resp := &pbgroup.QuitGroupResp{}
	if req.UserID == "" {
		req.UserID = mcontext.GetOpUserID(ctx)
	} else {
		if err := authverify.CheckAccessV3(ctx, req.UserID, &s.config.Manager, &s.config.IMAdmin); err != nil {
			return nil, err
		}
	}
	member, err := s.db.TakeGroupMember(ctx, req.GroupID, req.UserID)
	if err != nil {
		return nil, err
	}
	if member.RoleLevel == constant.GroupOwner {
		return nil, errs.ErrNoPermission.WrapMsg("group owner can't quit")
	}
	if err := s.PopulateGroupMember(ctx, member); err != nil {
		return nil, err
	}
	err = s.db.DeleteGroupMember(ctx, req.GroupID, []string{req.UserID})
	if err != nil {
		return nil, err
	}
	_ = s.Notification.MemberQuitNotification(ctx, s.groupMemberDB2PB(member, 0))
	if err := s.deleteMemberAndSetConversationSeq(ctx, req.GroupID, []string{req.UserID}); err != nil {
		return nil, err
	}

	quitGroupConfig := &GroupEventCallbackConfig{
		CallbackUrl:       s.config.Callback.CallbackUrl,
		BeforeCreateGroup: s.config.Callback.CallbackBeforeMemberJoinGroup,
	}

	if err := CallbackQuitGroup(ctx, quitGroupConfig, req); err != nil {
		return nil, err
	}
	return resp, nil
}

func (s *groupServer) deleteMemberAndSetConversationSeq(ctx context.Context, groupID string, userIDs []string) error {
	conevrsationID := msgprocessor.GetConversationIDBySessionType(constant.SuperGroupChatType, groupID)
	maxSeq, err := s.msgRpcClient.GetConversationMaxSeq(ctx, conevrsationID)
	if err != nil {
		return err
	}
	return s.conversationRpcClient.SetConversationMaxSeq(ctx, userIDs, conevrsationID, maxSeq)
}

func (s *groupServer) SetGroupInfo(ctx context.Context, req *pbgroup.SetGroupInfoReq) (*pbgroup.SetGroupInfoResp, error) {
	var opMember *relationtb.GroupMemberModel
	if !authverify.IsAppManagerUid(ctx, &s.config.Manager, &s.config.IMAdmin) {
		var err error
		opMember, err = s.db.TakeGroupMember(ctx, req.GroupInfoForSet.GroupID, mcontext.GetOpUserID(ctx))
		if err != nil {
			return nil, err
		}
		if !(opMember.RoleLevel == constant.GroupOwner || opMember.RoleLevel == constant.GroupAdmin) {
			return nil, errs.ErrNoPermission.WrapMsg("no group owner or admin")
		}
		if err := s.PopulateGroupMember(ctx, opMember); err != nil {
			return nil, err
		}
	}

	beforeSetGroupInfoConfig := &GroupEventCallbackConfig{
		CallbackUrl:       s.config.Callback.CallbackUrl,
		BeforeCreateGroup: s.config.Callback.CallbackBeforeMemberJoinGroup,
	}

	if err := CallbackBeforeSetGroupInfo(ctx, beforeSetGroupInfoConfig, req); err != nil {
		return nil, err
	}
	group, err := s.db.TakeGroup(ctx, req.GroupInfoForSet.GroupID)
	if err != nil {
		return nil, err
	}
	if group.Status == constant.GroupStatusDismissed {
		return nil, errs.ErrDismissedAlready.Wrap()
	}
	resp := &pbgroup.SetGroupInfoResp{}

	count, err := s.db.FindGroupMemberNum(ctx, group.GroupID)
	if err != nil {
		return nil, err
	}
	owner, err := s.db.TakeGroupOwner(ctx, group.GroupID)
	if err != nil {
		return nil, err
	}
	if err := s.PopulateGroupMember(ctx, owner); err != nil {
		return nil, err
	}
	update := UpdateGroupInfoMap(ctx, req.GroupInfoForSet)
	if len(update) == 0 {
		return resp, nil
	}
	if err := s.db.UpdateGroup(ctx, group.GroupID, update); err != nil {
		return nil, err
	}
	group, err = s.db.TakeGroup(ctx, req.GroupInfoForSet.GroupID)
	if err != nil {
		return nil, err
	}
	tips := &sdkws.GroupInfoSetTips{
		Group:    s.groupDB2PB(group, owner.UserID, count),
		MuteTime: 0,
		OpUser:   &sdkws.GroupMemberFullInfo{},
	}
	if opMember != nil {
		tips.OpUser = s.groupMemberDB2PB(opMember, 0)
	}
	num := len(update)
	if req.GroupInfoForSet.Notification != "" {
		num--
		func() {
			conversation := &pbconversation.ConversationReq{
				ConversationID:   msgprocessor.GetConversationIDBySessionType(constant.SuperGroupChatType, req.GroupInfoForSet.GroupID),
				ConversationType: constant.SuperGroupChatType,
				GroupID:          req.GroupInfoForSet.GroupID,
			}
			resp, err := s.GetGroupMemberUserIDs(ctx, &pbgroup.GetGroupMemberUserIDsReq{GroupID: req.GroupInfoForSet.GroupID})
			if err != nil {
				log.ZWarn(ctx, "GetGroupMemberIDs", err)
				return
			}
			conversation.GroupAtType = &wrapperspb.Int32Value{Value: constant.GroupNotification}
			if err := s.conversationRpcClient.SetConversations(ctx, resp.UserIDs, conversation); err != nil {
				log.ZWarn(ctx, "SetConversations", err, resp.UserIDs, conversation)
			}
		}()
		_ = s.Notification.GroupInfoSetAnnouncementNotification(ctx, &sdkws.GroupInfoSetAnnouncementTips{Group: tips.Group, OpUser: tips.OpUser})
	}
	if req.GroupInfoForSet.GroupName != "" {
		num--
		_ = s.Notification.GroupInfoSetNameNotification(ctx, &sdkws.GroupInfoSetNameTips{Group: tips.Group, OpUser: tips.OpUser})
	}
	if num > 0 {
		_ = s.Notification.GroupInfoSetNotification(ctx, tips)
	}

	afterSetGroupInfoConfig := beforeSetGroupInfoConfig
	if err := CallbackAfterSetGroupInfo(ctx, afterSetGroupInfoConfig, req); err != nil {
		return nil, err
	}
	return resp, nil
}

func (s *groupServer) TransferGroupOwner(ctx context.Context, req *pbgroup.TransferGroupOwnerReq) (*pbgroup.TransferGroupOwnerResp, error) {
	resp := &pbgroup.TransferGroupOwnerResp{}
	group, err := s.db.TakeGroup(ctx, req.GroupID)
	if err != nil {
		return nil, err
	}
	if group.Status == constant.GroupStatusDismissed {
		return nil, errs.ErrDismissedAlready.Wrap()
	}
	if req.OldOwnerUserID == req.NewOwnerUserID {
		return nil, errs.ErrArgs.WrapMsg("OldOwnerUserID == NewOwnerUserID")
	}
	members, err := s.db.FindGroupMembers(ctx, req.GroupID, []string{req.OldOwnerUserID, req.NewOwnerUserID})
	if err != nil {
		return nil, err
	}
	if err := s.PopulateGroupMember(ctx, members...); err != nil {
		return nil, err
	}
	memberMap := utils.SliceToMap(members, func(e *relationtb.GroupMemberModel) string { return e.UserID })
	if ids := utils.Single([]string{req.OldOwnerUserID, req.NewOwnerUserID}, utils.Keys(memberMap)); len(ids) > 0 {
		return nil, errs.ErrArgs.WrapMsg("user not in group " + strings.Join(ids, ","))
	}
	oldOwner := memberMap[req.OldOwnerUserID]
	if oldOwner == nil {
		return nil, errs.ErrArgs.WrapMsg("OldOwnerUserID not in group " + req.NewOwnerUserID)
	}
	newOwner := memberMap[req.NewOwnerUserID]
	if newOwner == nil {
		return nil, errs.ErrArgs.WrapMsg("NewOwnerUser not in group " + req.NewOwnerUserID)
	}
	if !authverify.IsAppManagerUid(ctx, &s.config.Manager, &s.config.IMAdmin) {
		if !(mcontext.GetOpUserID(ctx) == oldOwner.UserID && oldOwner.RoleLevel == constant.GroupOwner) {
			return nil, errs.ErrNoPermission.WrapMsg("no permission transfer group owner")
		}
	}
	if err := s.db.TransferGroupOwner(ctx, req.GroupID, req.OldOwnerUserID, req.NewOwnerUserID, newOwner.RoleLevel); err != nil {
		return nil, err
	}

	afterTransferGroupOwnerConfig := &GroupEventCallbackConfig{
		CallbackUrl:       s.config.Callback.CallbackUrl,
		BeforeCreateGroup: s.config.Callback.CallbackBeforeMemberJoinGroup,
	}

	if err := CallbackAfterTransferGroupOwner(ctx, afterTransferGroupOwnerConfig, req); err != nil {
		return nil, err
	}
	s.Notification.GroupOwnerTransferredNotification(ctx, req)
	return resp, nil
}

func (s *groupServer) GetGroups(ctx context.Context, req *pbgroup.GetGroupsReq) (*pbgroup.GetGroupsResp, error) {
	resp := &pbgroup.GetGroupsResp{}
	var (
		group []*relationtb.GroupModel
		err   error
	)

	if req.GroupID != "" {
		group, err = s.db.FindGroup(ctx, []string{req.GroupID})
		resp.Total = uint32(len(group))
	} else {
		var total int64
		total, group, err = s.db.SearchGroup(ctx, req.GroupName, req.Pagination)
		resp.Total = uint32(total)
	}

	if err != nil {
		return nil, err
	}

	groupIDs := utils.Slice(group, func(e *relationtb.GroupModel) string {
		return e.GroupID
	})

	ownerMembers, err := s.db.FindGroupsOwner(ctx, groupIDs)
	if err != nil {
		return nil, err
	}

	ownerMemberMap := utils.SliceToMap(ownerMembers, func(e *relationtb.GroupMemberModel) string {
		return e.GroupID
	})
	groupMemberNumMap, err := s.db.MapGroupMemberNum(ctx, groupIDs)
	if err != nil {
		return nil, err
	}
	resp.Groups = utils.Slice(group, func(group *relationtb.GroupModel) *pbgroup.CMSGroup {
		var (
			userID   string
			username string
		)
		if member, ok := ownerMemberMap[group.GroupID]; ok {
			userID = member.UserID
			username = member.Nickname
		}
		return convert.Db2PbCMSGroup(group, userID, username, groupMemberNumMap[group.GroupID])
	})
	return resp, nil
}

func (s *groupServer) GetGroupMembersCMS(ctx context.Context, req *pbgroup.GetGroupMembersCMSReq) (*pbgroup.GetGroupMembersCMSResp, error) {
	resp := &pbgroup.GetGroupMembersCMSResp{}
	total, members, err := s.db.SearchGroupMember(ctx, req.UserName, req.GroupID, req.Pagination)
	if err != nil {
		return nil, err
	}
	resp.Total = uint32(total)
	if err := s.PopulateGroupMember(ctx, members...); err != nil {
		return nil, err
	}
	resp.Members = utils.Slice(members, func(e *relationtb.GroupMemberModel) *sdkws.GroupMemberFullInfo {
		return convert.Db2PbGroupMember(e)
	})
	return resp, nil
}

func (s *groupServer) GetUserReqApplicationList(ctx context.Context, req *pbgroup.GetUserReqApplicationListReq) (*pbgroup.GetUserReqApplicationListResp, error) {
	resp := &pbgroup.GetUserReqApplicationListResp{}
	user, err := s.User.GetPublicUserInfo(ctx, req.UserID)
	if err != nil {
		return nil, err
	}
	total, requests, err := s.db.PageGroupRequestUser(ctx, req.UserID, req.Pagination)
	if err != nil {
		return nil, err
	}
	resp.Total = uint32(total)
	if len(requests) == 0 {
		return resp, nil
	}
	groupIDs := utils.Distinct(utils.Slice(requests, func(e *relationtb.GroupRequestModel) string {
		return e.GroupID
	}))
	groups, err := s.db.FindGroup(ctx, groupIDs)
	if err != nil {
		return nil, err
	}
	groupMap := utils.SliceToMap(groups, func(e *relationtb.GroupModel) string {
		return e.GroupID
	})
	owners, err := s.db.FindGroupsOwner(ctx, groupIDs)
	if err != nil {
		return nil, err
	}
	if err := s.PopulateGroupMember(ctx, owners...); err != nil {
		return nil, err
	}
	ownerMap := utils.SliceToMap(owners, func(e *relationtb.GroupMemberModel) string {
		return e.GroupID
	})
	groupMemberNum, err := s.db.MapGroupMemberNum(ctx, groupIDs)
	if err != nil {
		return nil, err
	}
	resp.GroupRequests = utils.Slice(requests, func(e *relationtb.GroupRequestModel) *sdkws.GroupRequest {
		var ownerUserID string
		if owner, ok := ownerMap[e.GroupID]; ok {
			ownerUserID = owner.UserID
		}
		return convert.Db2PbGroupRequest(e, user, convert.Db2PbGroupInfo(groupMap[e.GroupID], ownerUserID, groupMemberNum[e.GroupID]))
	})
	return resp, nil
}

func (s *groupServer) DismissGroup(ctx context.Context, req *pbgroup.DismissGroupReq) (*pbgroup.DismissGroupResp, error) {
	resp := &pbgroup.DismissGroupResp{}
	owner, err := s.db.TakeGroupOwner(ctx, req.GroupID)
	if err != nil {
		return nil, err
	}
	if !authverify.IsAppManagerUid(ctx, &s.config.Manager, &s.config.IMAdmin) {
		if owner.UserID != mcontext.GetOpUserID(ctx) {
			return nil, errs.ErrNoPermission.WrapMsg("not group owner")
		}
	}
	if err := s.PopulateGroupMember(ctx, owner); err != nil {
		return nil, err
	}
	group, err := s.db.TakeGroup(ctx, req.GroupID)
	if err != nil {
		return nil, err
	}
	if !req.DeleteMember && group.Status == constant.GroupStatusDismissed {
		return nil, errs.ErrDismissedAlready.WrapMsg("group status is dismissed")
	}
	if err := s.db.DismissGroup(ctx, req.GroupID, req.DeleteMember); err != nil {
		return nil, err
	}
	if !req.DeleteMember {
		num, err := s.db.FindGroupMemberNum(ctx, req.GroupID)
		if err != nil {
			return nil, err
		}
		tips := &sdkws.GroupDismissedTips{
			Group:  s.groupDB2PB(group, owner.UserID, num),
			OpUser: &sdkws.GroupMemberFullInfo{},
		}
		if mcontext.GetOpUserID(ctx) == owner.UserID {
			tips.OpUser = s.groupMemberDB2PB(owner, 0)
		}
		s.Notification.GroupDismissedNotification(ctx, tips)
	}
	membersID, err := s.db.FindGroupMemberUserID(ctx, group.GroupID)
	if err != nil {
		return nil, err
	}
	reqCall := &callbackstruct.CallbackDisMissGroupReq{
		GroupID:   req.GroupID,
		OwnerID:   owner.UserID,
		MembersID: membersID,
		GroupType: string(group.GroupType),
	}

	dismissGroupConfig := &GroupEventCallbackConfig{
		CallbackUrl:       s.config.Callback.CallbackUrl,
		BeforeCreateGroup: s.config.Callback.CallbackBeforeMemberJoinGroup,
	}

	if err := CallbackDismissGroup(ctx, dismissGroupConfig, reqCall); err != nil {
		return nil, err
	}

	return resp, nil
}

func (s *groupServer) MuteGroupMember(ctx context.Context, req *pbgroup.MuteGroupMemberReq) (*pbgroup.MuteGroupMemberResp, error) {
	resp := &pbgroup.MuteGroupMemberResp{}
	member, err := s.db.TakeGroupMember(ctx, req.GroupID, req.UserID)
	if err != nil {
		return nil, err
	}
	if err := s.PopulateGroupMember(ctx, member); err != nil {
		return nil, err
	}
	if !authverify.IsAppManagerUid(ctx, &s.config.Manager, &s.config.IMAdmin) {
		opMember, err := s.db.TakeGroupMember(ctx, req.GroupID, mcontext.GetOpUserID(ctx))
		if err != nil {
			return nil, err
		}
		switch member.RoleLevel {
		case constant.GroupOwner:
			return nil, errs.ErrNoPermission.WrapMsg("set group owner mute")
		case constant.GroupAdmin:
			if opMember.RoleLevel != constant.GroupOwner {
				return nil, errs.ErrNoPermission.WrapMsg("set group admin mute")
			}
		case constant.GroupOrdinaryUsers:
			if !(opMember.RoleLevel == constant.GroupAdmin || opMember.RoleLevel == constant.GroupOwner) {
				return nil, errs.ErrNoPermission.WrapMsg("set group ordinary users mute")
			}
		}
	}
	data := UpdateGroupMemberMutedTimeMap(time.Now().Add(time.Second * time.Duration(req.MutedSeconds)))
	if err := s.db.UpdateGroupMember(ctx, member.GroupID, member.UserID, data); err != nil {
		return nil, err
	}
	s.Notification.GroupMemberMutedNotification(ctx, req.GroupID, req.UserID, req.MutedSeconds)
	return resp, nil
}

func (s *groupServer) CancelMuteGroupMember(ctx context.Context, req *pbgroup.CancelMuteGroupMemberReq) (*pbgroup.CancelMuteGroupMemberResp, error) {
	member, err := s.db.TakeGroupMember(ctx, req.GroupID, req.UserID)
	if err != nil {
		return nil, err
	}
	if err := s.PopulateGroupMember(ctx, member); err != nil {
		return nil, err
	}
	if !authverify.IsAppManagerUid(ctx, &s.config.Manager, &s.config.IMAdmin) {
		opMember, err := s.db.TakeGroupMember(ctx, req.GroupID, mcontext.GetOpUserID(ctx))
		if err != nil {
			return nil, err
		}
		switch member.RoleLevel {
		case constant.GroupOwner:
			return nil, errs.ErrNoPermission.WrapMsg("set group owner mute")
		case constant.GroupAdmin:
			if opMember.RoleLevel != constant.GroupOwner {
				return nil, errs.ErrNoPermission.WrapMsg("set group admin mute")
			}
		case constant.GroupOrdinaryUsers:
			if !(opMember.RoleLevel == constant.GroupAdmin || opMember.RoleLevel == constant.GroupOwner) {
				return nil, errs.ErrNoPermission.WrapMsg("set group ordinary users mute")
			}
		}
	}
	data := UpdateGroupMemberMutedTimeMap(time.Unix(0, 0))
	if err := s.db.UpdateGroupMember(ctx, member.GroupID, member.UserID, data); err != nil {
		return nil, err
	}
	s.Notification.GroupMemberCancelMutedNotification(ctx, req.GroupID, req.UserID)
	return &pbgroup.CancelMuteGroupMemberResp{}, nil
}

func (s *groupServer) MuteGroup(ctx context.Context, req *pbgroup.MuteGroupReq) (*pbgroup.MuteGroupResp, error) {
	resp := &pbgroup.MuteGroupResp{}
	if err := s.CheckGroupAdmin(ctx, req.GroupID); err != nil {
		return nil, err
	}
	if err := s.db.UpdateGroup(ctx, req.GroupID, UpdateGroupStatusMap(constant.GroupStatusMuted)); err != nil {
		return nil, err
	}
	s.Notification.GroupMutedNotification(ctx, req.GroupID)
	return resp, nil
}

func (s *groupServer) CancelMuteGroup(ctx context.Context, req *pbgroup.CancelMuteGroupReq) (*pbgroup.CancelMuteGroupResp, error) {
	resp := &pbgroup.CancelMuteGroupResp{}
	if err := s.CheckGroupAdmin(ctx, req.GroupID); err != nil {
		return nil, err
	}
	if err := s.db.UpdateGroup(ctx, req.GroupID, UpdateGroupStatusMap(constant.GroupOk)); err != nil {
		return nil, err
	}
	s.Notification.GroupCancelMutedNotification(ctx, req.GroupID)
	return resp, nil
}

func (s *groupServer) SetGroupMemberInfo(ctx context.Context, req *pbgroup.SetGroupMemberInfoReq) (*pbgroup.SetGroupMemberInfoResp, error) {
	resp := &pbgroup.SetGroupMemberInfoResp{}
	if len(req.Members) == 0 {
		return nil, errs.ErrArgs.WrapMsg("members empty")
	}
	opUserID := mcontext.GetOpUserID(ctx)
	if opUserID == "" {
		return nil, errs.ErrNoPermission.WrapMsg("no op user id")
	}
	isAppManagerUid := authverify.IsAppManagerUid(ctx, &s.config.Manager, &s.config.IMAdmin)
	for i := range req.Members {
		req.Members[i].FaceURL = nil
	}
	groupMembers := make(map[string][]*pbgroup.SetGroupMemberInfo)
	for i, member := range req.Members {
		if member.RoleLevel != nil {
			switch member.RoleLevel.Value {
			case constant.GroupOwner:
				return nil, errs.ErrNoPermission.WrapMsg("cannot set ungroup owner")
			case constant.GroupAdmin, constant.GroupOrdinaryUsers:
			default:
				return nil, errs.ErrArgs.WrapMsg("invalid role level")
			}
		}
		groupMembers[member.GroupID] = append(groupMembers[member.GroupID], req.Members[i])
	}
	for groupID, members := range groupMembers {
		temp := make(map[string]struct{})
		userIDs := make([]string, 0, len(members)+1)
		for _, member := range members {
			if _, ok := temp[member.UserID]; ok {
				return nil, errs.ErrArgs.WrapMsg(fmt.Sprintf("repeat group %s user %s", member.GroupID, member.UserID))
			}
			temp[member.UserID] = struct{}{}
			userIDs = append(userIDs, member.UserID)
		}
		if _, ok := temp[opUserID]; !ok {
			userIDs = append(userIDs, opUserID)
		}
		dbMembers, err := s.db.FindGroupMembers(ctx, groupID, userIDs)
		if err != nil {
			return nil, err
		}
		opUserIndex := -1
		for i, member := range dbMembers {
			if member.UserID == opUserID {
				opUserIndex = i
				break
			}
		}
		switch len(userIDs) - len(dbMembers) {
		case 0:
			if !isAppManagerUid {
				roleLevel := dbMembers[opUserIndex].RoleLevel
				if roleLevel != constant.GroupOwner {
					switch roleLevel {
					case constant.GroupAdmin:
						for _, member := range dbMembers {
							if member.RoleLevel == constant.GroupOwner {
								return nil, errs.ErrNoPermission.WrapMsg("admin can not change group owner")
							}
							if member.RoleLevel == constant.GroupAdmin && member.UserID != opUserID {
								return nil, errs.ErrNoPermission.WrapMsg("admin can not change other group admin")
							}
						}
					case constant.GroupOrdinaryUsers:
						for _, member := range dbMembers {
							if !(member.RoleLevel == constant.GroupOrdinaryUsers && member.UserID == opUserID) {
								return nil, errs.ErrNoPermission.WrapMsg("ordinary users can not change other role level")
							}
						}
					default:
						for _, member := range dbMembers {
							if member.RoleLevel >= roleLevel {
								return nil, errs.ErrNoPermission.WrapMsg("can not change higher role level")
							}
						}
					}
				}
			}
		case 1:
			if opUserIndex >= 0 {
				return nil, errs.ErrArgs.WrapMsg("user not in group")
			}
			if !isAppManagerUid {
				return nil, errs.ErrNoPermission.WrapMsg("user not in group")
			}
		default:
			return nil, errs.ErrArgs.WrapMsg("user not in group")
		}
	}

	beforeSetGroupMemberInfoConfig := &GroupEventCallbackConfig{
		CallbackUrl:       s.config.Callback.CallbackUrl,
		BeforeCreateGroup: s.config.Callback.CallbackBeforeMemberJoinGroup,
	}

	for i := 0; i < len(req.Members); i++ {
		if err := CallbackBeforeSetGroupMemberInfo(ctx, beforeSetGroupMemberInfoConfig, req.Members[i]); err != nil {
			return nil, err
		}
	}
	if err := s.db.UpdateGroupMembers(ctx, utils.Slice(req.Members, func(e *pbgroup.SetGroupMemberInfo) *relationtb.BatchUpdateGroupMember {
		return &relationtb.BatchUpdateGroupMember{
			GroupID: e.GroupID,
			UserID:  e.UserID,
			Map:     UpdateGroupMemberMap(e),
		}
	})); err != nil {
		return nil, err
	}
	for _, member := range req.Members {
		if member.RoleLevel != nil {
			switch member.RoleLevel.Value {
			case constant.GroupAdmin:
				s.Notification.GroupMemberSetToAdminNotification(ctx, member.GroupID, member.UserID)
			case constant.GroupOrdinaryUsers:
				s.Notification.GroupMemberSetToOrdinaryUserNotification(ctx, member.GroupID, member.UserID)
			}
		}
		if member.Nickname != nil || member.FaceURL != nil || member.Ex != nil {
			s.Notification.GroupMemberInfoSetNotification(ctx, member.GroupID, member.UserID)
		}
	}
	afterSetGroupMemberInfoConfig := beforeSetGroupMemberInfoConfig
	for i := 0; i < len(req.Members); i++ {
		if err := CallbackAfterSetGroupMemberInfo(ctx, afterSetGroupMemberInfoConfig, req.Members[i]); err != nil {
			return nil, err
		}
	}

	return resp, nil
}

func (s *groupServer) GetGroupAbstractInfo(ctx context.Context, req *pbgroup.GetGroupAbstractInfoReq) (*pbgroup.GetGroupAbstractInfoResp, error) {
	resp := &pbgroup.GetGroupAbstractInfoResp{}
	if len(req.GroupIDs) == 0 {
		return nil, errs.ErrArgs.WrapMsg("groupIDs empty")
	}
	if utils.Duplicate(req.GroupIDs) {
		return nil, errs.ErrArgs.WrapMsg("groupIDs duplicate")
	}
	groups, err := s.db.FindGroup(ctx, req.GroupIDs)
	if err != nil {
		return nil, err
	}
	if ids := utils.Single(req.GroupIDs, utils.Slice(groups, func(group *relationtb.GroupModel) string {
		return group.GroupID
	})); len(ids) > 0 {
		return nil, errs.ErrGroupIDNotFound.WrapMsg("not found group " + strings.Join(ids, ","))
	}
	groupUserMap, err := s.db.MapGroupMemberUserID(ctx, req.GroupIDs)
	if err != nil {
		return nil, err
	}
	if ids := utils.Single(req.GroupIDs, utils.Keys(groupUserMap)); len(ids) > 0 {
		return nil, errs.ErrGroupIDNotFound.WrapMsg(fmt.Sprintf("group %s not found member", strings.Join(ids, ",")))
	}
	resp.GroupAbstractInfos = utils.Slice(groups, func(group *relationtb.GroupModel) *pbgroup.GroupAbstractInfo {
		users := groupUserMap[group.GroupID]
		return convert.Db2PbGroupAbstractInfo(group.GroupID, users.MemberNum, users.Hash)
	})
	return resp, nil
}

func (s *groupServer) GetUserInGroupMembers(ctx context.Context, req *pbgroup.GetUserInGroupMembersReq) (*pbgroup.GetUserInGroupMembersResp, error) {
	resp := &pbgroup.GetUserInGroupMembersResp{}
	if len(req.GroupIDs) == 0 {
		return nil, errs.ErrArgs.WrapMsg("groupIDs empty")
	}
	members, err := s.db.FindGroupMemberUser(ctx, req.GroupIDs, req.UserID)
	if err != nil {
		return nil, err
	}
	if err := s.PopulateGroupMember(ctx, members...); err != nil {
		return nil, err
	}
	resp.Members = utils.Slice(members, func(e *relationtb.GroupMemberModel) *sdkws.GroupMemberFullInfo {
		return convert.Db2PbGroupMember(e)
	})
	return resp, nil
}

func (s *groupServer) GetGroupMemberUserIDs(ctx context.Context, req *pbgroup.GetGroupMemberUserIDsReq) (resp *pbgroup.GetGroupMemberUserIDsResp, err error) {
	resp = &pbgroup.GetGroupMemberUserIDsResp{}
	resp.UserIDs, err = s.db.FindGroupMemberUserID(ctx, req.GroupID)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (s *groupServer) GetGroupMemberRoleLevel(ctx context.Context, req *pbgroup.GetGroupMemberRoleLevelReq) (*pbgroup.GetGroupMemberRoleLevelResp, error) {
	resp := &pbgroup.GetGroupMemberRoleLevelResp{}
	if len(req.RoleLevels) == 0 {
		return nil, errs.ErrArgs.WrapMsg("RoleLevels empty")
	}
	members, err := s.db.FindGroupMemberRoleLevels(ctx, req.GroupID, req.RoleLevels)
	if err != nil {
		return nil, err
	}
	if err := s.PopulateGroupMember(ctx, members...); err != nil {
		return nil, err
	}
	resp.Members = utils.Slice(members, func(e *relationtb.GroupMemberModel) *sdkws.GroupMemberFullInfo {
		return convert.Db2PbGroupMember(e)
	})
	return resp, nil
}

func (s *groupServer) GetGroupUsersReqApplicationList(ctx context.Context, req *pbgroup.GetGroupUsersReqApplicationListReq) (*pbgroup.GetGroupUsersReqApplicationListResp, error) {
	resp := &pbgroup.GetGroupUsersReqApplicationListResp{}
	requests, err := s.db.FindGroupRequests(ctx, req.GroupID, req.UserIDs)
	if err != nil {
		return nil, err
	}
	if len(requests) == 0 {
		return resp, nil
	}
	groupIDs := utils.Distinct(utils.Slice(requests, func(e *relationtb.GroupRequestModel) string {
		return e.GroupID
	}))
	groups, err := s.db.FindGroup(ctx, groupIDs)
	if err != nil {
		return nil, err
	}
	groupMap := utils.SliceToMap(groups, func(e *relationtb.GroupModel) string {
		return e.GroupID
	})
	if ids := utils.Single(groupIDs, utils.Keys(groupMap)); len(ids) > 0 {
		return nil, errs.ErrGroupIDNotFound.WrapMsg(strings.Join(ids, ","))
	}
	owners, err := s.db.FindGroupsOwner(ctx, groupIDs)
	if err != nil {
		return nil, err
	}
	if err := s.PopulateGroupMember(ctx, owners...); err != nil {
		return nil, err
	}
	ownerMap := utils.SliceToMap(owners, func(e *relationtb.GroupMemberModel) string {
		return e.GroupID
	})
	groupMemberNum, err := s.db.MapGroupMemberNum(ctx, groupIDs)
	if err != nil {
		return nil, err
	}
	resp.GroupRequests = utils.Slice(requests, func(e *relationtb.GroupRequestModel) *sdkws.GroupRequest {
		var ownerUserID string
		if owner, ok := ownerMap[e.GroupID]; ok {
			ownerUserID = owner.UserID
		}
		return convert.Db2PbGroupRequest(e, nil, convert.Db2PbGroupInfo(groupMap[e.GroupID], ownerUserID, groupMemberNum[e.GroupID]))
	})
	resp.Total = int64(len(resp.GroupRequests))
	return resp, nil
}
