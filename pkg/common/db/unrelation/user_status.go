package unrelation

import (
	"context"
	"github.com/OpenIMSDK/tools/errs"
	"github.com/openimsdk/open-im-server/v3/pkg/common/cachekey"
	"github.com/openimsdk/open-im-server/v3/pkg/common/db/table/unrelation"
	"github.com/redis/go-redis/v9"
	"time"
)

func NewUserStatus(rdb redis.UniversalClient) unrelation.UserModelInterface {
	return &UserStatus{rdb: rdb}
}

type UserStatus struct {
	rdb redis.UniversalClient
}

func str2any(userIDs []string) []any {
	res := make([]any, len(userIDs))
	for i, userID := range userIDs {
		res[i] = userID
	}
	return res
}

func (u *UserStatus) subscriptionKey(userID string) string {
	return cachekey.GetSubscriptionKey(userID)
}

func (u *UserStatus) subscribedKey(userID string) string {
	return cachekey.GetSubscribedKey(userID)
}

func (u *UserStatus) GetUserStateConnKey(userID string) string {
	return cachekey.GetUserStateConnKey(userID)
}

func (u *UserStatus) GetUserStatePlatformKey(userID string) string {
	return cachekey.GetUserStatePlatformKey(userID)
}

func (u *UserStatus) AddSubscriptionList(ctx context.Context, userID string, userIDList []string) error {
	script := `
local userIDs = {}
for i = 3, #ARGV do
    table.insert(userIDs, ARGV[i])
	redis.call("SADD", KEYS[2] .. ARGV[i], ARGV[1])
    redis.call("EXPIRE", KEYS[2] .. ARGV[i], ARGV[2])
end
redis.call("SADD", KEYS[1] .. ARGV[1], unpack(userIDs))
redis.call("EXPIRE", KEYS[1] .. ARGV[1], ARGV[2])
return 1
`
	expiration := time.Hour
	keys := []string{cachekey.SubscriptionKey, cachekey.SubscribedKey}
	argv := make([]any, 0, len(userIDList)+2)
	argv = append(argv, userID, expiration.Seconds())
	for _, uid := range userIDList {
		argv = append(argv, uid)
	}
	return u.rdb.Eval(ctx, script, keys, argv...).Err()
}

func (u *UserStatus) UnsubscriptionList(ctx context.Context, userID string, userIDList []string) error {
	return errs.Wrap(u.rdb.SRem(ctx, u.subscriptionKey(userID), str2any(userIDList)...).Err())
}

// GetAllSubscribeList 我订阅的用户
func (u *UserStatus) GetAllSubscribeList(ctx context.Context, userID string) (userIDList []string, err error) {
	return u.rdb.SMembers(ctx, u.subscriptionKey(userID)).Result()
}

// GetSubscribedList 订阅我的用户
func (u *UserStatus) GetSubscribedList(ctx context.Context, userID string) (userIDList []string, err error) {
	return u.rdb.SMembers(ctx, u.subscribedKey(userID)).Result()
}

func (u *UserStatus) SetUserOnline(ctx context.Context, userID string, connID string, platformID int32) (bool, error) {
	script := `
local exist = redis.call("HSETNX", KEYS[1], ARGV[1], ARGV[2])
redis.call("EXPIRE", KEYS[1], ARGV[3])
if not exist then
	return 1
end
local value = redis.call("HINCRBY", KEYS[2], ARGV[2], 1)
redis.call("EXPIRE", KEYS[2], ARGV[3])
if value == 1 then
	return 2
else
	return 3
end
`
	keys := []string{u.GetUserStateConnKey(userID), u.GetUserStatePlatformKey(userID)}
	argv := []any{connID, platformID, time.Hour.Seconds()}
	val, err := u.rdb.Eval(ctx, script, keys, argv...).Int64()
	if err != nil {
		return false, err
	}
	return val == 2, nil
}

func (u *UserStatus) SetUserOffline(ctx context.Context, userID string, platformID int32) error {

	return nil
}
