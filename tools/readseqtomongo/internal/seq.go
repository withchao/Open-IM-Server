package internal

import (
	"context"
	"errors"
	"fmt"
	"github.com/openimsdk/open-im-server/v3/pkg/common/cmd"
	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database/mgo"
	"github.com/openimsdk/tools/db/mongoutil"
	"github.com/openimsdk/tools/db/redisutil"
	"github.com/redis/go-redis/v9"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const match = "SEQ_USER_READ:*"

const timeout = time.Second * 500

func Start(path string) error {
	redisConfig, err := readConfig[config.Redis](path, cmd.RedisConfigFileName)
	if err != nil {
		return err
	}
	mongodbConfig, err := readConfig[config.Mongo](path, cmd.MongodbConfigFileName)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	rdb, err := redisutil.NewRedisClient(ctx, redisConfig.Build())
	if err != nil {
		return err
	}
	mgocli, err := mongoutil.NewMongoDB(ctx, mongodbConfig.Build())
	if err != nil {
		return err
	}
	seqUser, err := mgo.NewSeqUserMongo(mgocli.GetDB())
	if err != nil {
		return err
	}
	return scanRead(rdb, seqUser)
}

func scanRead(rdb redis.UniversalClient, seqUser database.SeqUser) error {
	var (
		cursor uint64
		keys   []string
		err    error
	)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		keys, cursor, err = rdb.Scan(ctx, cursor, match, 100).Result()
		cancel()
		if err != nil {
			return fmt.Errorf("redis scan %w", err)
		}
		for _, key := range keys {
			if err := handlerKey(rdb, seqUser, key); err != nil {
				return fmt.Errorf("handler key %s failed %w", key, err)
			}
		}
		if cursor == 0 {
			return nil
		}
	}
}

func handlerKey(rdb redis.UniversalClient, seqUser database.SeqUser, key string) error {
	// SEQ_USER_READ:si_1322850833_5790931854:5790931854
	arr := strings.Split(key, ":")
	if len(arr) != 3 {
		return fmt.Errorf("invalid key")
	}
	userID := arr[2]
	conversationID := arr[1]
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	seq, err := rdb.HGet(ctx, key, "value").Int64()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil
		}
		return err
	}
	return seqUser.SetUserReadSeq(ctx, conversationID, userID, seq)
}

func readConfig[T any](dir string, name string) (*T, error) {
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return nil, err
	}
	var conf T
	if err := yaml.Unmarshal(data, &conf); err != nil {
		return nil, err
	}
	return &conf, nil
}
