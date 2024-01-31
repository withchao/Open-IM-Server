package unrelation

import (
	"context"
	"github.com/OpenIMSDK/tools/errs"
	"github.com/OpenIMSDK/tools/pagination"
	"github.com/OpenIMSDK/tools/utils"
	"github.com/dtm-labs/rockscache"
	"github.com/openimsdk/open-im-server/v3/pkg/common/cachekey"
	"github.com/openimsdk/open-im-server/v3/pkg/common/db/table/unrelation"
	"github.com/redis/go-redis/v9"
	"strconv"
	"time"
)

const redisPlaceholder = "$placeholder$"

func NewUserStatus(rdb redis.UniversalClient, getGroupMemberID func(ctx context.Context, groupID string) ([]string, error)) unrelation.UserModelInterface {
	return &UserStatus{
		rdb:                    rdb,
		rocks:                  rockscache.NewClient(rdb, rockscache.NewDefaultOptions()),
		subscriptionExpiration: time.Hour * 1,
		onlineExpiration:       time.Hour * 24,
		groupExpiration:        time.Hour * 1,
	}
}

type UserStatus struct {
	rdb                    redis.UniversalClient
	rocks                  *rockscache.Client
	subscriptionExpiration time.Duration
	onlineExpiration       time.Duration
	groupExpiration        time.Duration
	getGroupMemberID       func(ctx context.Context, groupID string) ([]string, error)
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

func (u *UserStatus) GetGroupStateKey(groupID string) string {
	return cachekey.GetGroupStateKey(groupID)
}

func (u *UserStatus) GetGroupStateTagKey(groupID string) string {
	return cachekey.GetGroupStateTagKey(groupID)
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
	keys := []string{cachekey.SubscriptionKey, cachekey.SubscribedKey}
	argv := make([]any, 0, len(userIDList)+2)
	argv = append(argv, userID, u.subscriptionExpiration.Seconds())
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
local target = tostring(ARGV[1])

local exist = redis.call("HSETNX", KEYS[1], KEYS[2], target)
redis.call("EXPIRE", KEYS[1], ARGV[2])
if exist == 0 then
	return 0
end

local count = 0
for _, value in ipairs(redis.call("HVALS", KEYS[1])) do
    if value == target then
        count = count + 1
    end
end
return count
`
	keys := []string{u.GetUserStateConnKey(userID), connID}
	argv := []any{platformID, u.onlineExpiration.Seconds()}
	val, err := u.rdb.Eval(ctx, script, keys, argv...).Int64()
	if err != nil {
		return false, errs.Wrap(err)
	}
	return val == 1, nil
}

func (u *UserStatus) SetUserOffline(ctx context.Context, userID string, connID string) (bool, error) {
	script := `
local platformID = redis.call("HGET", KEYS[1], KEYS[2])
if platformID == false or platformID == nil then
	return -1
end
redis.call("HDEL", KEYS[1], KEYS[2])

local count = 0
for _, value in ipairs(redis.call("HVALS", KEYS[1])) do
    if value == platformID then
        count = count + 1
    end
end
return count
`
	keys := []string{u.GetUserStateConnKey(userID), connID}
	val, err := u.rdb.Eval(ctx, script, keys).Int64()
	if err != nil {
		return false, errs.Wrap(err)
	}
	return val == 0, nil
}

func (u *UserStatus) GetUserOnline(ctx context.Context, userID string) ([]int32, error) {
	vals, err := u.rdb.HVals(ctx, u.GetUserStateConnKey(userID)).Result()
	if err != nil {
		return nil, errs.Wrap(err)
	}
	tmp := make(map[string]struct{})
	platformIDs := make([]int32, 0, len(vals))
	for _, val := range vals {
		if _, ok := tmp[val]; ok {
			continue
		}
		tmp[val] = struct{}{}
		if v, err := strconv.Atoi(val); err == nil {
			platformIDs = append(platformIDs, int32(v))
		}
	}
	utils.Sort(platformIDs, true)
	return platformIDs, nil
}

func (u *UserStatus) SetGroupOnline(ctx context.Context, userID string, online bool, groupIDs []string) error {
	if len(groupIDs) == 0 {
		return nil
	}
	keys := make([]string, 0, len(groupIDs))
	for _, groupID := range groupIDs {
		keys = append(keys, u.GetGroupStateKey(groupID))
	}
	if online {
		return u.setGroupOnline(ctx, userID, keys)
	} else {
		return u.setGroupOffline(ctx, userID, keys)
	}
}

func (u *UserStatus) setGroupOnline(ctx context.Context, userID string, keys []string) error {
	script := `
for i = 1, #KEYS do
    redis.call("ZADD", KEYS[i], ARGV[2], ARGV[1])
end
return 1
`
	argv := []any{userID, u.getScore()}
	return errs.Wrap(u.rdb.Eval(ctx, script, keys, argv...).Err())
}

func (u *UserStatus) setGroupOffline(ctx context.Context, userID string, keys []string) error {
	script := `
for i = 1, #KEYS do
    redis.call("ZREM", KEYS[i], ARGV[1])
end
return 1
`
	argv := []any{userID}
	return errs.Wrap(u.rdb.Eval(ctx, script, keys, argv...).Err())
}

func (u *UserStatus) getScore() int64 {
	return time.Now().Unix()
}

func (u *UserStatus) GetGroupOnline(ctx context.Context, groupID string, desc bool, pagination pagination.Pagination) (int64, []string, error) {
	if err := u.initGroupOnline(ctx, groupID); err != nil {
		return 0, nil, err
	}
	key := u.GetGroupStateKey(groupID)
	total, err := u.rdb.ZCard(ctx, key).Result()
	if err != nil {
		return 0, nil, err
	}
	if total > 0 {
		total--
	}
	var start, end int64
	if desc {
		start = -int64(pagination.GetPageNumber()) * int64(pagination.GetShowNumber())
		end = start + int64(pagination.GetShowNumber()) - 1
	} else {
		start = int64((pagination.GetPageNumber()-1)*pagination.GetShowNumber()) + 1
		end = start + int64(pagination.GetShowNumber())
	}
	userIDs, err := u.rdb.ZRange(ctx, key, start, end).Result()
	if err != nil {
		return 0, nil, err
	}
	if desc && len(userIDs) > 0 {
		if userIDs[0] == redisPlaceholder {
			userIDs = userIDs[1:]
		}
		reverse(userIDs)
	}
	return total, userIDs, nil
}

func reverse[T any](list []T) {
	for i, j := 0, len(list)-1; i < j; i, j = i+1, j-1 {
		list[i], list[j] = list[j], list[i]
	}
}

func (u *UserStatus) initGroupOnline(ctx context.Context, groupID string) error {
	_, err := u.rocks.Fetch(u.GetGroupStateTagKey(groupID), u.groupExpiration, func() (string, error) {
		userIDs, err := u.getGroupMemberID(ctx, groupID)
		if err != nil {
			return "", err
		}
		score := u.getScore()
		argv := make([]any, 0, len(userIDs)/2)
		argv = append(argv, u.groupExpiration.Seconds(), score)
		for _, userID := range userIDs {
			platformIDs, err := u.GetUserOnline(ctx, userID)
			if err != nil {
				return "", err
			}
			if len(platformIDs) > 0 {
				argv = append(argv, userID)
			}
		}
		if err := u.initGroupOnlineRedis(ctx, u.GetGroupStateKey(groupID), argv); err != nil {
			return "", err
		}
		return strconv.Itoa(int(score)), nil
	})
	return err
}

func (u *UserStatus) initGroupOnlineRedis(ctx context.Context, key string, argv []any) error {
	script := `
redis.call("DEL", KEYS[1])
redis.call("ZADD", KEYS[1], 0, KEYS[2])
redis.call("EXPIRE", KEYS[1], ARGV[1])
for i = 3, #ARGV do
    redis.call("ZADD", KEYS[1], ARGV[2], ARGV[i])
end
return 1
`
	keys := []string{key, redisPlaceholder}
	return errs.Wrap(u.rdb.Eval(ctx, script, keys, argv...).Err())
}
