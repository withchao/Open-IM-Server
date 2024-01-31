package unrelation

import (
	"context"
	"github.com/OpenIMSDK/protocol/sdkws"
	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/redis/go-redis/v9"
	"strconv"
	"testing"
)

func TestName(t *testing.T) {
	config.Config.Redis.Address = []string{"vm.czor.top:16379"}
	config.Config.Redis.Password = "openIM123"

	rdb := redis.NewClient(&redis.Options{
		Addr:     "172.16.8.38:16379",
		Password: "openIM123",
		DB:       5,
	})

	u := NewUserStatus(rdb).(*UserStatus)

	var userIDs []string
	for i := 0; i < 100; i++ {
		userIDs = append(userIDs, strconv.Itoa(10000+i))
	}

	getGroupMemberIDs := func(ctx context.Context, groupID string) ([]string, error) {
		return userIDs, nil
	}

	u.getGroupMemberID = getGroupMemberIDs

	var groupID = "333333"
	//
	//if err := u.setGroupOnline(context.Background(), "10000", []string{groupID}); err != nil {
	//	panic(err)
	//}
	//return

	for _, userID := range userIDs {
		_, err := u.SetUserOnline(context.Background(), userID, "cid:"+userID, 9)
		if err != nil {
			panic(err)
		}
	}

	pagination := &sdkws.RequestPagination{PageNumber: 1, ShowNumber: 200}

	//userIDs = nil

	for _, userID := range userIDs {
		if err := u.SetGroupInfo(context.Background(), userID, true, []string{groupID}); err != nil {
			panic(err)
		}
	}

	total, userIDs, err := u.GetGroupOnline(context.Background(), groupID, !false, pagination)
	if err != nil {
		panic(err)
	}
	t.Log(total)
	t.Log(userIDs)

}

func TestName111(t *testing.T) {
	arr := []int{1, 2, 3, 4, 5, 6, 7, 8, 9}
	reverse(arr)
	t.Log(arr)

}
