package mw

import (
	"context"
	"errors"
	"github.com/OpenIMSDK/Open-IM-Server/pkg/common/constant"
	"github.com/OpenIMSDK/Open-IM-Server/pkg/common/log"
	"github.com/OpenIMSDK/Open-IM-Server/pkg/errs"
	"github.com/OpenIMSDK/Open-IM-Server/pkg/proto/errinfo"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"strings"
)

func GrpcClient() grpc.DialOption {
	return grpc.WithUnaryInterceptor(rpcClientInterceptor)
}

func rpcClientInterceptor(ctx context.Context, method string, req, resp interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) (err error) {
	if ctx == nil {
		return errs.ErrInternalServer.Wrap("call rpc request context is nil")
	}
	log.ZInfo(ctx, "rpc client req", "funcName", method, "req", rpcString(req))
	operationID, ok := ctx.Value(constant.OperationID).(string)
	if !ok {
		log.ZWarn(ctx, "ctx missing operationID", errors.New("ctx missing operationID"), "funcName", method)
		return errs.ErrArgs.Wrap("ctx missing operationID")
	}
	md := metadata.Pairs(constant.OperationID, operationID)
	opUserID, ok := ctx.Value(constant.OpUserID).(string)
	if ok {
		md.Append(constant.OpUserID, opUserID)
	}
	opUserIDPlatformID, ok := ctx.Value(constant.OpUserPlatform).(string)
	if ok {
		md.Append(constant.OpUserPlatform, opUserIDPlatformID)
	}
	err = invoker(metadata.NewOutgoingContext(ctx, md), method, req, resp, cc, opts...)
	if err == nil {
		log.ZInfo(ctx, "rpc client resp", "funcName", method, "resp", rpcString(resp))
		return nil
	}
	log.ZError(ctx, "rpc resp error", err)
	rpcErr, ok := err.(interface{ GRPCStatus() *status.Status })
	if !ok {
		return errs.ErrInternalServer.Wrap(err.Error())
	}
	sta := rpcErr.GRPCStatus()
	if sta.Code() == 0 {
		return errs.NewCodeError(errs.ServerInternalError, err.Error()).Wrap()
	}
	if details := sta.Details(); len(details) > 0 {
		errInfo, ok := details[0].(*errinfo.ErrorInfo)
		if ok {
			s := strings.Join(errInfo.Warp, "->") + errInfo.Cause
			return errs.NewCodeError(int(sta.Code()), sta.Message()).WithDetail(s).Wrap()
		}
	}
	return errs.NewCodeError(int(sta.Code()), sta.Message()).Wrap()
}
