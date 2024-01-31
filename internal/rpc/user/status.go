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

func (s *userServer) getUserStatus(ctx context.Context, userIDs []string) ([]*pbuser.OnlineStatus, error) {
	statusList := make([]*pbuser.OnlineStatus, 0, len(userIDs))
	for _, userID := range userIDs {
		platformIDs, err := s.UserDatabase.GetUserOnline(ctx, userID)
		if err != nil {
			return nil, err
		}
		status := pbuser.OnlineStatus{
			UserID:      userID,
			PlatformIDs: platformIDs,
		}
		if len(platformIDs) > 0 {
			status.Status = constant.Online
		} else {
			status.Status = constant.Offline
		}
		statusList = append(statusList, &status)
	}
	return statusList, nil
}

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
		status, err = s.getUserStatus(ctx, req.UserIDs)
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
	statusList, err := s.getUserStatus(ctx, userList)
	if err != nil {
		return nil, err
	}
	return &pbuser.GetSubscribeUsersStatusResp{StatusList: statusList}, nil
}

func (s *userServer) UserStatusChangeNotification(ctx context.Context, userID string, status int32, platformID int32) {
	list, err := s.UserDatabase.GetSubscribedList(ctx, userID)
	if err != nil {
		log.ZError(ctx, "GetSubscribedList err", err)
		return
	}
	for _, uid := range list {
		tips := &sdkws.UserStatusChangeTips{
			FromUserID: userID,
			ToUserID:   uid,
			Status:     status,
			PlatformID: platformID,
		}
		s.userNotificationSender.UserStatusChangeNotification(ctx, tips)
	}
}

// SetUserStatus Synchronize user's online status.
func (s *userServer) SetUserStatus(ctx context.Context, req *pbuser.SetUserStatusReq) (*pbuser.SetUserStatusResp, error) {
	var (
		first bool
		err   error
	)
	switch req.Status {
	case constant.Online:
		first, err = s.UserDatabase.SetUserOnline(ctx, req.UserID, req.ConnID, req.PlatformID)
	case constant.Offline:
		first, err = s.UserDatabase.SetUserOffline(ctx, req.UserID, req.ConnID)
	default:
		err = errs.ErrArgs.Wrap("status invalid")
	}
	if err != nil {
		return nil, err
	}
	if first {
		if err := s.updateGroupCache(ctx, req.UserID); err != nil {
			log.ZError(ctx, "updateGroupCache err", err, "userID", req.UserID)
		}
		s.UserStatusChangeNotification(ctx, req.UserID, req.Status, req.PlatformID)
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
	}
	return &pbuser.SetUserStatusResp{}, nil
}

func (s *userServer) updateGroupCache(ctx context.Context, userID string) error {
	groupIDs, err := s.groupLocalCache.GetJoinedGroupIDs(ctx, userID)
	if err != nil {
		return err
	}
	if len(groupIDs) == 0 {
		return nil
	}
	platformIDs, err := s.UserDatabase.GetUserOnline(ctx, userID)
	if err != nil {
		return err
	}
	online := len(platformIDs) > 0
	return s.UserDatabase.SetGroupOnline(ctx, userID, online, groupIDs)
}

// GetUserStatus Get the online status of the user.
func (s *userServer) GetUserStatus(ctx context.Context, req *pbuser.GetUserStatusReq) (resp *pbuser.GetUserStatusResp, err error) {
	statusList, err := s.getUserStatus(ctx, req.UserIDs)
	if err != nil {
		return nil, err
	}
	return &pbuser.GetUserStatusResp{StatusList: statusList}, nil
}

func (s *userServer) GetGroupOnlineUser(ctx context.Context, req *pbuser.GetGroupOnlineUserReq) (*pbuser.GetGroupOnlineUserResp, error) {
	if req.Pagination == nil {
		return nil, errs.ErrArgs.Wrap("pagination is nil")
	}
	total, userIDs, err := s.UserDatabase.GetGroupOnline(ctx, req.GroupID, req.Desc, req.Pagination)
	if err != nil {
		return nil, err
	}
	return &pbuser.GetGroupOnlineUserResp{
		Total:   total,
		UserIDs: userIDs,
	}, nil
}
