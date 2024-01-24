package unrelation

import (
	"context"
	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/redis/go-redis/v9"
	"testing"
)

func TestName(t *testing.T) {
	config.Config.Redis.Address = []string{"vm.czor.top:16379"}
	config.Config.Redis.Password = "openIM123"

	rdb := redis.NewClient(&redis.Options{
		Addr:     "vm.czor.top:16379",
		Password: "openIM123",
		DB:       5,
	})
	u := &UserStatus{rdb: rdb}
	//err := u.AddSubscriptionList(context.Background(), "111111", []string{"222222", "333333"})
	//t.Log(err)
	//
	//t.Log(u.GetSubscribedList(context.Background(), "111111"))
	//t.Log(u.GetSubscribedList(context.Background(), "222222"))
	//
	//t.Log(u.GetAllSubscribeList(context.Background(), "111111"))
	//t.Log(u.GetAllSubscribeList(context.Background(), "222222"))

	first, err := u.SetUserOnline(context.Background(), "111111", "123457", 2)
	t.Log(first, err)

}
