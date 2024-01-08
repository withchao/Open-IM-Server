package rpccache

import (
	"context"
	"github.com/openimsdk/open-im-server/v3/pkg/common/cachekey"
	"github.com/openimsdk/open-im-server/v3/pkg/common/localcache"
	"github.com/openimsdk/open-im-server/v3/pkg/rpcclient"
)

func NewFriendLocalCache(client rpcclient.FriendRpcClient) *FriendLocalCache {
	return &FriendLocalCache{
		local:  localcache.New[any](),
		client: client,
	}
}

type FriendLocalCache struct {
	local  localcache.Cache[any]
	client rpcclient.FriendRpcClient
}

func (f *FriendLocalCache) GetFriendIDs(ctx context.Context, ownerUserID string) ([]string, error) {
	return localcache.AnyValue[[]string](f.local.Get(ctx, cachekey.GetFriendIDsKey(ownerUserID), func(ctx context.Context) (any, error) {
		return f.client.GetFriendIDs(ctx, ownerUserID)
	}))
}

func (f *FriendLocalCache) IsFriend(ctx context.Context, possibleFriendUserID, userID string) (bool, error) {
	return localcache.AnyValue[bool](f.local.Get(ctx, cachekey.GetIsFriendKey(possibleFriendUserID, userID), func(ctx context.Context) (any, error) {
		return f.client.IsFriend(ctx, possibleFriendUserID, userID)
	}))
}

func (f *FriendLocalCache) IsBlocked(ctx context.Context, possibleBlackUserID, userID string) (bool, error) {
	return localcache.AnyValue[bool](f.local.Get(ctx, cachekey.GetIsBlackIDsKey(possibleBlackUserID, userID), func(ctx context.Context) (any, error) {
		return f.client.IsFriend(ctx, possibleBlackUserID, userID)
	}))
}
