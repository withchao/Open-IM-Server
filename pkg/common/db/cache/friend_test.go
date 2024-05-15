package cache

import (
	"bytes"
	"encoding/json"
	relationtb "github.com/openimsdk/open-im-server/v3/pkg/common/db/table/relation"
	"github.com/openimsdk/tools/utils/datautil"
	"math/rand"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestName(t *testing.T) {
	rand.Seed(time.Now().UnixMilli())
	fs := make([]*relationtb.FriendModel, 1000)
	for i := range fs {
		fs[i] = &relationtb.FriendModel{
			FriendUserID:   strconv.Itoa(rand.Int()),
			Remark:         strconv.Itoa(rand.Int()),
			CreateTime:     time.Now().Add(time.Hour * time.Duration(rand.Intn(10000))),
			AddSource:      int32(rand.Int()) % 100,
			OperatorUserID: strconv.Itoa(rand.Int()),
			Ex:             strconv.Itoa(rand.Int()),
			IsPinned:       rand.Int()%2 == 0,
		}
	}

	var b []byte
	for i := 0; ; i++ {
		rand.Shuffle(len(fs), func(i, j int) {
			fs[i], fs[j] = fs[j], fs[i]
		})
		datautil.SortAny(fs, func(a, b *relationtb.FriendModel) bool {
			return a.CreateTime.After(b.CreateTime)
		})
		tmp := datautil.Slice(fs, func(e *relationtb.FriendModel) string {
			return strings.Join(friendModel2Strings(e), ",")
		})
		tb, err := json.Marshal(tmp)
		if err != nil {
			panic(err)
		}
		if i == 0 {
			b = tb
			continue
		}
		if !bytes.Equal(b, tb) {
			t.Log("not equal", i)
			t.Log(string(b))
			t.Log(string(tb))
			return
		}
		if i%10000 == 0 {
			t.Log(i)
		}
	}

	//s := friendModel2Strings(fs[0])
	//
	//t.Log(fs, s)
}

func friendModel2Strings(f *relationtb.FriendModel) []string {
	return []string{
		f.FriendUserID,
		f.Remark,
		strconv.FormatInt(f.CreateTime.UnixMilli(), 10),
		strconv.Itoa(int(f.AddSource)),
		f.OperatorUserID,
		f.Ex,
		strconv.FormatBool(f.IsPinned),
	}
}
