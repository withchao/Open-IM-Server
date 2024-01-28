package unrelation

import (
	"context"
	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/redis/go-redis/v9"
	"testing"
	"time"
)

func TestName(t *testing.T) {
	config.Config.Redis.Address = []string{"vm.czor.top:16379"}
	config.Config.Redis.Password = "openIM123"

	rdb := redis.NewClient(&redis.Options{
		Addr:     "172.16.8.38:16379",
		Password: "openIM123",
		DB:       5,
	})
	u := &UserStatus{
		rdb:              rdb,
		onlineExpiration: time.Second * 99999999,
	}
	//err := u.AddSubscriptionList(context.Background(), "111111", []string{"222222", "333333"})
	//t.Log(err)
	//
	//t.Log(u.GetSubscribedList(context.Background(), "111111"))
	//t.Log(u.GetSubscribedList(context.Background(), "222222"))
	//
	//t.Log(u.GetAllSubscribeList(context.Background(), "111111"))
	//t.Log(u.GetAllSubscribeList(context.Background(), "222222"))

	t.Log(u.SetUserOnline(context.Background(), "111111", "c123451", 9))
	t.Log(u.SetUserOnline(context.Background(), "111111", "c123452", 8))
	t.Log(u.SetUserOnline(context.Background(), "111111", "c123453", 9))

	t.Log(u.GetUserOnline(context.Background(), "111111"))

	//t.Log(u.SetUserOffline(context.Background(), "111111", "c123451"))
	//t.Log(u.SetUserOffline(context.Background(), "111111", "c123452"))
	//t.Log(u.SetUserOffline(context.Background(), "111111", "c123453"))

	//arr, err := u.rdb.HVals(context.Background(), "aaaaaaa").Result()
	//if err != nil {
	//	panic(err)
	//}
	//t.Log(arr)
}
