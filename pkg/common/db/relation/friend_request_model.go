package relation

import (
	"Open_IM/pkg/common/db/table/relation"
	"Open_IM/pkg/common/tracelog"
	"Open_IM/pkg/utils"
	"context"
	"gorm.io/gorm"
)

//var FriendRequestDB *gorm.DB

func NewFriendRequestGorm(db *gorm.DB) *FriendRequestGorm {
	var fr FriendRequestGorm
	fr.DB = db
	return &fr
}

type FriendRequestGorm struct {
	DB *gorm.DB `gorm:"-"`
}

func (f *FriendRequestGorm) Create(ctx context.Context, friends []*relation.FriendRequestModel) (err error) {
	defer func() {
		tracelog.SetCtxDebug(ctx, utils.GetSelfFuncName(), err, "friends", friends)
	}()
	return utils.Wrap(f.DB.Model(&relation.FriendRequestModel{}).Create(&friends).Error, "")
}

func (f *FriendRequestGorm) Delete(ctx context.Context, fromUserID, toUserID string) (err error) {
	defer func() {
		tracelog.SetCtxDebug(ctx, utils.GetSelfFuncName(), err, "fromUserID", fromUserID, "toUserID", toUserID)
	}()
	return utils.Wrap(f.DB.Model(&relation.FriendRequestModel{}).Where("from_user_id = ? and to_user_id = ?", fromUserID, toUserID).Delete(&relation.FriendRequestModel{}).Error, "")
}

func (f *FriendRequestGorm) UpdateByMap(ctx context.Context, ownerUserID string, args map[string]interface{}) (err error) {
	defer func() {
		tracelog.SetCtxDebug(ctx, utils.GetSelfFuncName(), err, "ownerUserID", ownerUserID, "args", args)
	}()
	return utils.Wrap(f.DB.Model(&relation.FriendRequestModel{}).Where("owner_user_id = ?", ownerUserID).Updates(args).Error, "")
}

func (f *FriendRequestGorm) Update(ctx context.Context, friends []*relation.FriendRequestModel) (err error) {
	defer func() {
		tracelog.SetCtxDebug(ctx, utils.GetSelfFuncName(), err, "friends", friends)
	}()
	return utils.Wrap(f.DB.Model(&relation.FriendRequestModel{}).Updates(&friends).Error, "")
}

func (f *FriendRequestGorm) Find(ctx context.Context, ownerUserID string) (friends []*relation.FriendRequestModel, err error) {
	defer func() {
		tracelog.SetCtxDebug(ctx, utils.GetSelfFuncName(), err, "ownerUserID", ownerUserID, "friends", friends)
	}()
	return friends, utils.Wrap(f.DB.Model(&relation.FriendRequestModel{}).Where("owner_user_id = ?", ownerUserID).Find(&friends).Error, "")
}

func (f *FriendRequestGorm) Take(ctx context.Context, fromUserID, toUserID string) (friend *relation.FriendRequestModel, err error) {
	friend = &relation.FriendRequestModel{}
	defer tracelog.SetCtxDebug(ctx, utils.GetSelfFuncName(), err, "fromUserID", fromUserID, "toUserID", toUserID, "friend", friend)
	return friend, utils.Wrap(f.DB.Model(&relation.FriendRequestModel{}).Where("from_user_id = ? and to_user_id", fromUserID, toUserID).Take(friend).Error, "")
}

func (f *FriendRequestGorm) FindToUserID(ctx context.Context, toUserID string) (friends []*relation.FriendRequestModel, err error) {
	defer func() {
		tracelog.SetCtxDebug(ctx, utils.GetSelfFuncName(), err, "toUserID", toUserID, "friends", friends)
	}()
	return friends, utils.Wrap(f.DB.Model(&relation.FriendRequestModel{}).Where("to_user_id = ?", toUserID).Find(&friends).Error, "")
}

func (f *FriendRequestGorm) FindFromUserID(ctx context.Context, fromUserID string) (friends []*relation.FriendRequestModel, err error) {
	defer func() {
		tracelog.SetCtxDebug(ctx, utils.GetSelfFuncName(), err, "fromUserID", fromUserID, "friends", friends)
	}()
	return friends, utils.Wrap(f.DB.Model(&relation.FriendRequestModel{}).Where("from_user_id = ?", fromUserID).Find(&friends).Error, "")
}
