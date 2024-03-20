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

package startrpc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/openimsdk/tools/log"

	grpcprometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	"github.com/openimsdk/tools/discoveryregistry"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/mw"
	"github.com/openimsdk/tools/network"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	config2 "github.com/openimsdk/open-im-server/v3/pkg/common/config"
	kdisc "github.com/openimsdk/open-im-server/v3/pkg/common/discoveryregister"
	"github.com/openimsdk/open-im-server/v3/pkg/common/prommetrics"
	util "github.com/openimsdk/open-im-server/v3/pkg/util/genutil"
)

// Start rpc server.
func Start(ctx context.Context, rpcPort int, rpcRegisterName string, prometheusPort int, config *config2.GlobalConfig, rpcFn func(ctx context.Context, config *config.GlobalConfig, client discoveryregistry.SvcDiscoveryRegistry, server *grpc.Server) error, options ...grpc.ServerOption) error {
	log.CInfo(ctx, "RPC server is initializing", "rpcRegisterName", rpcRegisterName, "rpcPort", rpcPort,
		"prometheusPort", prometheusPort)
	rpcTcpAddr := net.JoinHostPort(network.GetListenIP(config.Rpc.ListenIP), strconv.Itoa(rpcPort))
	listener, err := net.Listen(
		"tcp",
		rpcTcpAddr,
	)
	if err != nil {
		return errs.WrapMsg(err, "listen err", "rpcTcpAddr", rpcTcpAddr)
	}

	defer listener.Close()
	client, err := kdisc.NewDiscoveryRegister(config)
	if err != nil {
		return err
	}

	defer client.Close()
	client.AddOption(mw.GrpcClient(), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithDefaultServiceConfig(fmt.Sprintf(`{"LoadBalancingPolicy": "%s"}`, "round_robin")))
	registerIP, err := network.GetRpcRegisterIP(config.Rpc.RegisterIP)
	if err != nil {
		return err
	}

	var reg *prometheus.Registry
	var metric *grpcprometheus.ServerMetrics
	if config.Prometheus.Enable {
		cusMetrics := prommetrics.GetGrpcCusMetrics(rpcRegisterName, &config.RpcRegisterName)
		reg, metric, _ = prommetrics.NewGrpcPromObj(cusMetrics)
		options = append(options, mw.GrpcServer(), grpc.StreamInterceptor(metric.StreamServerInterceptor()),
			grpc.UnaryInterceptor(metric.UnaryServerInterceptor()))
	} else {
		options = append(options, mw.GrpcServer())
	}

	srv := grpc.NewServer(options...)
	once := sync.Once{}
	defer func() {
		once.Do(srv.GracefulStop)
	}()

	err = rpcFn(ctx, config, client, srv)
	if err != nil {
		return err
	}

	err = client.Register(
		rpcRegisterName,
		registerIP,
		rpcPort,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return err
	}

	var (
		netDone    = make(chan struct{}, 2)
		netErr     error
		httpServer *http.Server
	)

	go func() {
		if config.Prometheus.Enable && prometheusPort != 0 {
			metric.InitializeMetrics(srv)
			// Create a HTTP server for prometheus.
			httpServer = &http.Server{Handler: promhttp.HandlerFor(reg, promhttp.HandlerOpts{}), Addr: fmt.Sprintf("0.0.0.0:%d", prometheusPort)}
			if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				netErr = errs.WrapMsg(err, "prometheus start err", httpServer.Addr)
				netDone <- struct{}{}
			}
		}
	}()

	go func() {
		err := srv.Serve(listener)
		if err != nil {
			netErr = errs.WrapMsg(err, "rpc start err: ", rpcTcpAddr)
			netDone <- struct{}{}
		}
	}()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM)
	select {
	case <-sigs:
		util.SIGTERMExit()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := gracefulStopWithCtx(ctx, srv.GracefulStop); err != nil {
			return err
		}
		ctx, cancel = context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		err := httpServer.Shutdown(ctx)
		if err != nil {
			return errs.WrapMsg(err, "shutdown err")
		}
		return nil
	case <-netDone:
		close(netDone)
		return netErr
	}
}

func gracefulStopWithCtx(ctx context.Context, f func()) error {
	done := make(chan struct{}, 1)
	go func() {
		f()
		close(done)
	}()
	select {
	case <-ctx.Done():
		return errs.Wrap(errors.New("timeout, ctx graceful stop"))
	case <-done:
		return nil
	}
}
