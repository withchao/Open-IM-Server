package cache

import (
	"context"
	"fmt"
	"github.com/dtm-labs/rockscache"
	"github.com/openimsdk/open-im-server/v3/pkg/common/cachekey"
	"github.com/openimsdk/open-im-server/v3/pkg/common/db/table/relation"
	"github.com/openimsdk/tools/errs"
	"github.com/redis/go-redis/v9"
	"strconv"
	"time"
)

const (
	seqUserCacheTime     = time.Hour
	seqUserReadCacheTime = time.Hour * 24 * 3
	seqUserReadLockTime  = time.Second * 10
	seqUserReadWriteStep = 20
)

type SeqUser interface {
	SetUserMaxSeq(ctx context.Context, conversationID string, userID string, maxSeq int64) error
	SetUserMinSeq(ctx context.Context, conversationID string, userID string, minSeq int64) error
	GetUserMaxSeq(ctx context.Context, conversationID string, userID string) (int64, error)
	GetUserMinSeq(ctx context.Context, conversationID string, userID string) (int64, error)
	SetUserReadSeq(ctx context.Context, conversationID string, userID string, seq int64) error
	GetUserReadSeq(ctx context.Context, conversationID string, userID string) (int64, error)
}

func NewSeqUserCache(rdb redis.UniversalClient, mgo relation.SeqUserModelInterface) *seqUserCache {
	opt := rockscache.NewDefaultOptions()
	opt.EmptyExpire = time.Second * 3
	opt.Delay = time.Second / 2
	return &seqUserCache{
		rdb:   rdb,
		mgo:   mgo,
		rocks: rockscache.NewClient(rdb, opt),
	}
}

type seqUserCache struct {
	rdb   redis.UniversalClient
	rocks *rockscache.Client
	mgo   relation.SeqUserModelInterface
}

func (s *seqUserCache) SetUserMaxSeq(ctx context.Context, conversationID string, userID string, maxSeq int64) error {
	if err := s.mgo.SetMaxSeq(ctx, conversationID, userID, maxSeq); err != nil {
		return err
	}
	if err := s.rocks.TagAsDeleted2(ctx, cachekey.GetSeqUserMaxSeqKey(conversationID, userID)); err != nil {
		return errs.Wrap(err)
	}
	return nil
}

func (s *seqUserCache) SetUserMinSeq(ctx context.Context, conversationID string, userID string, minSeq int64) error {
	if err := s.mgo.SetMinSeq(ctx, conversationID, userID, minSeq); err != nil {
		return err
	}
	if err := s.rocks.TagAsDeleted2(ctx, cachekey.GetSeqUserMinSeqKey(conversationID, userID)); err != nil {
		return errs.Wrap(err)
	}
	return nil
}

func (s *seqUserCache) GetUserMaxSeq(ctx context.Context, conversationID string, userID string) (int64, error) {
	return getCache[int64](ctx, s.rocks, cachekey.GetSeqUserMaxSeqKey(conversationID, userID), seqUserCacheTime, func(ctx context.Context) (int64, error) {
		return s.mgo.GetMaxSeq(ctx, conversationID, userID)
	})
}

func (s *seqUserCache) GetUserMinSeq(ctx context.Context, conversationID string, userID string) (int64, error) {
	return getCache[int64](ctx, s.rocks, cachekey.GetSeqUserMinSeqKey(conversationID, userID), seqUserCacheTime, func(ctx context.Context) (int64, error) {
		return s.mgo.GetMaxSeq(ctx, conversationID, userID)
	})
}

func (s *seqUserCache) SetUserReadSeq(ctx context.Context, conversationID string, userID string, seq int64) error {
	state, err := s.handlerReadSeq(ctx, conversationID, userID, seq)
	if err != nil {
		return err
	}
	switch state {
	case 1: // cache does not exist
		return s.initReadSeq(ctx, conversationID, userID, seq)
	case 2: // cache is greater than or equal to seq
		return nil
	case 3: // temporary redis
		return nil
	case 4: // write to mongo
		return s.mgo.SetReadSeq(ctx, conversationID, userID, seq)
	default:
		return errs.Wrap(fmt.Errorf("unknown redis lua return state: %d", state))
	}
}

func (s *seqUserCache) GetUserReadSeq(ctx context.Context, conversationID string, userID string) (int64, error) {
	for i := 0; i < 2; i++ {
		res, err := s.rdb.HGet(ctx, cachekey.GetSeqUserReadSeqKey(conversationID, userID), "seq").Result()
		if err == nil {
			seq, err := strconv.ParseInt(res, 10, 64)
			if err != nil {
				return 0, errs.WrapMsg(err, "parse redis cache read seq error", "conversationID", conversationID, "userID", userID)
			}
			return seq, nil
		} else if err != redis.Nil {
			return 0, errs.Wrap(err)
		}
		if err := s.initReadSeq(ctx, conversationID, userID, -1); err != nil {
			return 0, err
		}
	}
	return 0, errs.Wrap(fmt.Errorf("get read seq failed, conversationID: %s, userID: %s", conversationID, userID))
}

func (s *seqUserCache) initReadSeq(ctx context.Context, conversationID string, userID string, seq int64) error {
	_, err := getCache[int64](ctx, s.rocks, cachekey.GetSeqUserReadLockSeqKey(conversationID, userID), seqUserReadLockTime, func(ctx context.Context) (int64, error) {
		dbSeq, err := s.mgo.GetReadSeq(ctx, conversationID, userID)
		if err != nil {
			return 0, err
		}
		if seq > 0 && seq > dbSeq {
			dbSeq = seq
		}
		if err := s.setReadSeq(ctx, conversationID, userID, dbSeq); err != nil {
			return 0, err
		}
		return dbSeq, nil
	})
	return err
}

func (s *seqUserCache) handlerReadSeq(ctx context.Context, conversationID string, userID string, seq int64) (int, error) {
	script := `
local seqStr = redis.call("HGET", KEYS[1], "seq")
if seqStr == false then
	return 1
end
if tonumber(seqStr) >= tonumber(ARGV[1]) then
	return 2
end
redis.call("HSET", KEYS[1], "seq", ARGV[1])
redis.call("EXPIRE", KEYS[1], ARGV[3])
if redis.call("HINCRBY", KEYS[1], "count", 1) % tonumber(ARGV[2]) ~= 0 then
	return 3
end
return 4
`
	res, err := s.rdb.Eval(ctx, script, []string{cachekey.GetSeqUserReadSeqKey(conversationID, userID)}, seq, seqUserReadWriteStep, seqUserReadCacheTime.Seconds()).Int()
	if err != nil {
		return 0, errs.Wrap(err)
	}
	return res, nil
}

func (s *seqUserCache) setReadSeq(ctx context.Context, conversationID string, userID string, seq int64) error {
	script := `
if redis.call("EXISTS", KEYS[1]) == 1 then
	return 0
end
redis.call("HSET", KEYS[1], "seq", ARGV[1])
redis.call("HSET", KEYS[1], "count", 1)
redis.call("EXPIRE", KEYS[1], ARGV[2])
return 1
`
	return errs.Wrap(s.rdb.Eval(ctx, script, []string{cachekey.GetSeqUserReadSeqKey(conversationID, userID)}, seq, seqUserReadCacheTime.Seconds()).Err())
}
