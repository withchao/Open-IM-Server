package user

import (
	"context"
	"github.com/OpenIMSDK/protocol/constant"
	"github.com/OpenIMSDK/protocol/sdkws"
	pbuser "github.com/OpenIMSDK/protocol/user"
	"github.com/OpenIMSDK/tools/errs"
	"github.com/OpenIMSDK/tools/log"
	"github.com/openimsdk/open-im-server/v3/internal/msggateway"
)

// SubscribeOrCancelUsersStatus Subscribe online or cancel online users.
func (s *userServer) SubscribeOrCancelUsersStatus(ctx context.Context, req *pbuser.SubscribeOrCancelUsersStatusReq) (resp *pbuser.SubscribeOrCancelUsersStatusResp, err error) {
	if !(req.Genre == constant.SubscriberUser || req.Genre == constant.Unsubscribe) {
		return nil, errs.ErrArgs.Wrap("genre invalid")
	}
	if req.Genre == constant.SubscriberUser {
		err = s.UserDatabase.SubscribeUsersStatus(ctx, req.UserID, req.UserIDs)
		if err != nil {
			return nil, err
		}
		var status []*pbuser.OnlineStatus
		status, err = s.UserDatabase.GetUserStatus(ctx, req.UserIDs)
		if err != nil {
			return nil, err
		}
		return &pbuser.SubscribeOrCancelUsersStatusResp{StatusList: status}, nil
	} else if req.Genre == constant.Unsubscribe {
		err = s.UserDatabase.UnsubscribeUsersStatus(ctx, req.UserID, req.UserIDs)
		if err != nil {
			return nil, err
		}
	}
	return &pbuser.SubscribeOrCancelUsersStatusResp{}, nil
}

// GetSubscribeUsersStatus Get the online status of subscribers.
func (s *userServer) GetSubscribeUsersStatus(ctx context.Context, req *pbuser.GetSubscribeUsersStatusReq) (*pbuser.GetSubscribeUsersStatusResp, error) {
	userList, err := s.UserDatabase.GetAllSubscribeList(ctx, req.UserID)
	if err != nil {
		return nil, err
	}
	onlineStatusList, err := s.UserDatabase.GetUserStatus(ctx, userList)
	if err != nil {
		return nil, err
	}
	return &pbuser.GetSubscribeUsersStatusResp{StatusList: onlineStatusList}, nil
}

// SetUserStatus Synchronize user's online status.
func (s *userServer) SetUserStatus(ctx context.Context, req *pbuser.SetUserStatusReq) (resp *pbuser.SetUserStatusResp, err error) {
	if !(req.Status == constant.Online || req.Status == constant.Offline) {
		return nil, errs.ErrArgs.Wrap("status invalid")
	}
	// setnx
	// userID + connID

	/*

			setnx
			userID + connID

			{
				"connID": "PlatformID",
			}

		    {
				"connID": NUM,
			}



	*/

	err = s.UserDatabase.SetUserStatus(ctx, req.UserID, req.Status, req.PlatformID)
	if err != nil {
		return nil, err
	}
	list, err := s.UserDatabase.GetSubscribedList(ctx, req.UserID)
	if err != nil {
		return nil, err
	}
	for _, userID := range list {
		tips := &sdkws.UserStatusChangeTips{
			FromUserID: req.UserID,
			ToUserID:   userID,
			Status:     req.Status,
			PlatformID: req.PlatformID,
		}
		s.userNotificationSender.UserStatusChangeNotification(ctx, tips)
	}
	switch req.Status {
	case constant.Online:
		err := msggateway.CallbackUserOnline(ctx, req.UserID, int(req.PlatformID), req.IsBackground, req.ConnID)
		if err != nil {
			log.ZWarn(ctx, "CallbackUserOnline err", err)
		}
	case constant.Offline:
		err := msggateway.CallbackUserOffline(ctx, req.UserID, int(req.PlatformID), req.ConnID)
		if err != nil {
			log.ZWarn(ctx, "CallbackUserOffline err", err)
		}
	}
	return &pbuser.SetUserStatusResp{}, nil
}

// GetUserStatus Get the online status of the user.
func (s *userServer) GetUserStatus(ctx context.Context, req *pbuser.GetUserStatusReq) (resp *pbuser.GetUserStatusResp, err error) {
	onlineStatusList, err := s.UserDatabase.GetUserStatus(ctx, req.UserIDs)
	if err != nil {
		return nil, err
	}
	return &pbuser.GetUserStatusResp{StatusList: onlineStatusList}, nil
}
