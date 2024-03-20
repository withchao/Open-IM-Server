// Copyright © 2023 OpenIM. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package third

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/open-im-server/v3/pkg/common/db/cache"
	"github.com/openimsdk/open-im-server/v3/pkg/common/db/controller"
	"github.com/openimsdk/open-im-server/v3/pkg/common/db/mgo"
	"github.com/openimsdk/open-im-server/v3/pkg/common/db/s3"
	"github.com/openimsdk/open-im-server/v3/pkg/common/db/s3/cos"
	"github.com/openimsdk/open-im-server/v3/pkg/common/db/s3/minio"
	"github.com/openimsdk/open-im-server/v3/pkg/common/db/s3/oss"
	"github.com/openimsdk/open-im-server/v3/pkg/common/db/unrelation"
	"github.com/openimsdk/open-im-server/v3/pkg/rpcclient"
	"github.com/openimsdk/protocol/third"
	"github.com/openimsdk/tools/discoveryregistry"
	"github.com/openimsdk/tools/errs"
	"google.golang.org/grpc"
)

func Start(ctx context.Context, config *config.GlobalConfig, client discoveryregistry.SvcDiscoveryRegistry, server *grpc.Server) error {
	rdb, err := cache.NewRedis(ctx, &config.Redis)
	if err != nil {
		return err
	}
	mongo, err := unrelation.NewMongoDB(ctx, &config.Mongo)
	if err != nil {
		return err
	}
	logdb, err := mgo.NewLogMongo(mongo.GetDatabase(config.Mongo.Database))
	if err != nil {
		return err
	}
	s3db, err := mgo.NewS3Mongo(mongo.GetDatabase(config.Mongo.Database))
	if err != nil {
		return err
	}
	apiURL := config.Object.ApiURL
	if apiURL == "" {
		return errs.Wrap(fmt.Errorf("api is empty"))
	}
	if _, err := url.Parse(config.Object.ApiURL); err != nil {
		return err
	}
	if apiURL[len(apiURL)-1] != '/' {
		apiURL += "/"
	}
	apiURL += "object/"

	// Select the oss method according to the profile policy
	enable := config.Object.Enable
	var o s3.Interface
	switch enable {
	case "minio":
		o, err = minio.NewMinio(cache.NewMinioCache(rdb), minio.Config(config.Object.Minio))
	case "cos":
		o, err = cos.NewCos(cos.Config(config.Object.Cos))
	case "oss":
		o, err = oss.NewOSS(oss.Config(config.Object.Oss))
	default:
		err = fmt.Errorf("invalid object enable: %s", enable)
	}
	if err != nil {
		return err
	}
	third.RegisterThirdServer(server, &thirdServer{
		apiURL:        apiURL,
		thirdDatabase: controller.NewThirdDatabase(cache.NewMsgCacheModel(rdb, config.MsgCacheTimeout, &config.Redis), logdb),
		userRpcClient: rpcclient.NewUserRpcClient(client, config.RpcRegisterName.OpenImUserName, &config.Manager, &config.IMAdmin),
		s3dataBase:    controller.NewS3Database(rdb, o, s3db),
		defaultExpire: time.Hour * 24 * 7,
		config:        config,
	})
	return nil
}

type thirdServer struct {
	apiURL        string
	thirdDatabase controller.ThirdDatabase
	s3dataBase    controller.S3Database
	userRpcClient rpcclient.UserRpcClient
	defaultExpire time.Duration
	config        *config.GlobalConfig
}

func (t *thirdServer) FcmUpdateToken(ctx context.Context, req *third.FcmUpdateTokenReq) (resp *third.FcmUpdateTokenResp, err error) {
	err = t.thirdDatabase.FcmUpdateToken(ctx, req.Account, int(req.PlatformID), req.FcmToken, req.ExpireTime)
	if err != nil {
		return nil, err
	}
	return &third.FcmUpdateTokenResp{}, nil
}

func (t *thirdServer) SetAppBadge(ctx context.Context, req *third.SetAppBadgeReq) (resp *third.SetAppBadgeResp, err error) {
	err = t.thirdDatabase.SetAppBadge(ctx, req.UserID, int(req.AppUnreadCount))
	if err != nil {
		return nil, err
	}
	return &third.SetAppBadgeResp{}, nil
}
