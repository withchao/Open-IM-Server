package cache

import (
	"context"
	"errors"
	"github.com/OpenIMSDK/tools/errs"
	"github.com/dtm-labs/rockscache"
	"github.com/openimsdk/open-im-server/v3/pkg/common/cachekey"
	"github.com/openimsdk/open-im-server/v3/pkg/common/db/table/relation"
	"github.com/redis/go-redis/v9"
	"strconv"
	"time"
)

func NewSeqCache(rdb redis.UniversalClient) *seqCache {
	opt := rockscache.NewDefaultOptions()
	opt.EmptyExpire = time.Second * 3
	return &seqCache{
		rdb:        rdb,
		rocks:      rockscache.NewClient(rdb, opt),
		lockExpire: time.Minute * 10,
		seqExpire:  time.Hour * 24,
	}
}

type seqCache struct {
	rdb        redis.UniversalClient
	rocks      *rockscache.Client
	mgo        relation.SeqModelInterface
	lockExpire time.Duration
	seqExpire  time.Duration
	minNum     int64
}

func (s *seqCache) Malloc(ctx context.Context, conversationID string, size int64) ([]int64, error) {
	if size <= 0 {
		return nil, errs.Wrap(errors.New("size must be greater than 0"))
	}
	seqKey := cachekey.GetMallocSeq(conversationID)
	lockKey := cachekey.GetMallocSeqLock(conversationID)
	for i := 0; i < 10; i++ {
		seqs, err := s.lpop(ctx, seqKey, lockKey, size)
		if err != nil {
			return nil, err
		}
		if len(seqs) != int(size) {
			if err := s.mallocSeq(ctx, conversationID, size, &seqs); err != nil {
				return nil, err
			}
		}
		if len(seqs) == int(size) {
			return seqs, nil
		}
	}
	return nil, errs.ErrInternalServer.Wrap("malloc seq failed")
}

func (s *seqCache) push(ctx context.Context, seqKey string, seqs []int64) error {
	script := `
redis.call("DEL", KEYS[1])
for i = 2, #ARGV do
	redis.call("RPUSH", KEYS[1], ARGV[i]))
end
redis.call("EXPIRE", KEYS[1], ARGV[1])
return 1
`
	argv := make([]any, 0, 1+len(seqs))
	argv = append(argv, s.seqExpire.Seconds())
	for _, seq := range seqs {
		argv = append(argv, seq)
	}
	err := s.rdb.Eval(ctx, script, []string{seqKey}, argv...).Err()
	return errs.Wrap(err)
}

func (s *seqCache) lpop(ctx context.Context, seqKey, lockKey string, size int64) ([]int64, error) {
	script := `
local result = redis.call("LRANGE", KEYS[1], 0, ARGV[1])
if #result == 0 then
	return result
end
redis.call("LTRIM", KEYS[1], #result, -1)
if redis.call("LLEN", KEYS[1]) == 0 then
	redis.call("DEL", KEYS[2])
end
return result
`
	res, err := s.rdb.Eval(ctx, script, []string{seqKey, lockKey}, size).Int64Slice()
	if err != nil {
		return nil, errs.Wrap(err)
	}
	return res, nil
}

func (s *seqCache) mallocSeq(ctx context.Context, conversationID string, size int64, seqs *[]int64) error {
	_, err := getCache[string](ctx, s.rocks, cachekey.GetMallocSeqLock(conversationID), s.lockExpire, func(ctx context.Context) (string, error) {
		num := s.minNum
		if size > s.minNum {
			num += size
		}
		res, err := s.mgo.Malloc(ctx, conversationID, num)
		if err != nil {
			return "", err
		}
		if len(*seqs) > 0 && (*seqs)[len(*seqs)-1]+1 == res[0] {
			n := size - int64(len(res))
			*seqs = append(*seqs, res[:n]...)
			res = res[n:]
		} else {
			*seqs = res[:size]
			res = res[size:]
		}
		if err := s.push(ctx, cachekey.GetMallocSeq(conversationID), res); err != nil {
			return "", err
		}
		return strconv.Itoa(int(time.Now().UnixMicro())), nil
	})
	return err
}
