package msg

import (
	"Open_IM/internal/common/check"
	"Open_IM/internal/common/notification"
	"Open_IM/internal/common/rpcserver"
	"Open_IM/pkg/common/config"
	"Open_IM/pkg/common/constant"
	"Open_IM/pkg/common/db/controller"

	"Open_IM/pkg/common/kafka"
	"Open_IM/pkg/common/log"
	promePkg "Open_IM/pkg/common/prometheus"
	"Open_IM/pkg/proto/msg"
	"Open_IM/pkg/utils"
	"github.com/OpenIMSDK/getcdv3"
	"net"
	"strconv"
	"strings"

	grpcPrometheus "github.com/grpc-ecosystem/go-grpc-prometheus"

	"google.golang.org/grpc"
)

type msgServer struct {
	*rpcserver.RpcServer
	MsgInterface controller.MsgInterface
	Group        *check.GroupChecker
}

type deleteMsg struct {
	UserID      string
	OpUserID    string
	SeqList     []uint32
	OperationID string
}

func NewRpcChatServer(port int) *msgServer {
	log.NewPrivateLog(constant.LogFileName)
	rc := msgServer{
		rpcPort:         port,
		rpcRegisterName: config.Config.RpcRegisterName.OpenImMsgName,
		etcdSchema:      config.Config.Etcd.EtcdSchema,
		etcdAddr:        config.Config.Etcd.EtcdAddr,
		dMessageLocker:  NewLockerMessage(),
	}
	rc.messageWriter = kafka.NewKafkaProducer(config.Config.Kafka.Ws2mschat.Addr, config.Config.Kafka.Ws2mschat.Topic)
	//rc.offlineProducer = kafka.NewKafkaProducer(config.Config.Kafka.Ws2mschatOffline.Addr, config.Config.Kafka.Ws2mschatOffline.Topic)
	rc.delMsgCh = make(chan deleteMsg, 1000)
	return &rc
}

func (rpc *rpcChat) initPrometheus() {
	//sendMsgSuccessCounter = promauto.NewCounter(prometheus.CounterOpts{
	//	Name: "send_msg_success",
	//	Help: "The number of send msg success",
	//})
	//sendMsgFailedCounter = promauto.NewCounter(prometheus.CounterOpts{
	//	Name: "send_msg_failed",
	//	Help: "The number of send msg failed",
	//})
	promePkg.NewMsgPullFromRedisSuccessCounter()
	promePkg.NewMsgPullFromRedisFailedCounter()
	promePkg.NewMsgPullFromMongoSuccessCounter()
	promePkg.NewMsgPullFromMongoFailedCounter()

	promePkg.NewSingleChatMsgRecvSuccessCounter()
	promePkg.NewGroupChatMsgRecvSuccessCounter()
	promePkg.NewWorkSuperGroupChatMsgRecvSuccessCounter()

	promePkg.NewSingleChatMsgProcessSuccessCounter()
	promePkg.NewSingleChatMsgProcessFailedCounter()
	promePkg.NewGroupChatMsgProcessSuccessCounter()
	promePkg.NewGroupChatMsgProcessFailedCounter()
	promePkg.NewWorkSuperGroupChatMsgProcessSuccessCounter()
	promePkg.NewWorkSuperGroupChatMsgProcessFailedCounter()
}

func (m *msgServer) Run() {
	log.Info("", "rpcChat init...")
	listenIP := ""
	if config.Config.ListenIP == "" {
		listenIP = "0.0.0.0"
	} else {
		listenIP = config.Config.ListenIP
	}
	address := listenIP + ":" + strconv.Itoa(m.rpcPort)
	listener, err := net.Listen("tcp", address)
	if err != nil {
		panic("listening err:" + err.Error() + m.rpcRegisterName)
	}
	log.Info("", "listen network success, address ", address)
	recvSize := 1024 * 1024 * 30
	sendSize := 1024 * 1024 * 30
	var grpcOpts = []grpc.ServerOption{
		grpc.MaxRecvMsgSize(recvSize),
		grpc.MaxSendMsgSize(sendSize),
	}
	if config.Config.Prometheus.Enable {
		promePkg.NewGrpcRequestCounter()
		promePkg.NewGrpcRequestFailedCounter()
		promePkg.NewGrpcRequestSuccessCounter()
		grpcOpts = append(grpcOpts, []grpc.ServerOption{
			// grpc.UnaryInterceptor(promePkg.UnaryServerInterceptorProme),
			grpc.StreamInterceptor(grpcPrometheus.StreamServerInterceptor),
			grpc.UnaryInterceptor(grpcPrometheus.UnaryServerInterceptor),
		}...)
	}
	srv := grpc.NewServer(grpcOpts...)
	defer srv.GracefulStop()

	rpcRegisterIP := config.Config.RpcRegisterIP
	msg.RegisterMsgServer(srv, m)
	if config.Config.RpcRegisterIP == "" {
		rpcRegisterIP, err = utils.GetLocalIP()
		if err != nil {
			log.Error("", "GetLocalIP failed ", err.Error())
		}
	}
	err = getcdv3.RegisterEtcd(m.etcdSchema, strings.Join(m.etcdAddr, ","), rpcRegisterIP, m.rpcPort, m.rpcRegisterName, 10, "")
	if err != nil {
		log.Error("", "register rpcChat to etcd failed ", err.Error())
		panic(utils.Wrap(err, "register chat module  m to etcd err"))
	}
	go m.runCh()
	m.initPrometheus()
	err = srv.Serve(listener)
	if err != nil {
		log.Error("", "m rpcChat failed ", err.Error())
		return
	}
	log.Info("", "m rpcChat init success")
}

func (rpc *rpcChat) runCh() {
	log.NewInfo("", "start del msg chan ")
	for {
		select {
		case msg := <-rpc.delMsgCh:
			log.NewInfo(msg.OperationID, utils.GetSelfFuncName(), "delmsgch recv new: ", msg)
			db.DB.DelMsgFromCache(msg.UserID, msg.SeqList, msg.OperationID)
			unexistSeqList, err := db.DB.DelMsgBySeqList(msg.UserID, msg.SeqList, msg.OperationID)
			if err != nil {
				log.NewError(msg.OperationID, utils.GetSelfFuncName(), "DelMsgBySeqList args: ", msg.UserID, msg.SeqList, msg.OperationID, err.Error())
				continue
			}
			if len(unexistSeqList) > 0 {
				notification.DeleteMessageNotification(msg.OpUserID, msg.UserID, unexistSeqList, msg.OperationID)
			}
		}
	}
}
