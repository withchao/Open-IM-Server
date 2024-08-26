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

package push

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/IBM/sarama"
	"github.com/openimsdk/open-im-server/v3/internal/push/offlinepush"
	"github.com/openimsdk/open-im-server/v3/internal/push/offlinepush/options"
	"github.com/openimsdk/open-im-server/v3/pkg/common/prommetrics"
	"github.com/openimsdk/open-im-server/v3/pkg/common/webhook"
	"github.com/openimsdk/open-im-server/v3/pkg/msgprocessor"
	"github.com/openimsdk/open-im-server/v3/pkg/rpccache"
	"github.com/openimsdk/open-im-server/v3/pkg/rpcclient"
	"github.com/openimsdk/open-im-server/v3/pkg/util/conversationutil"
	"github.com/openimsdk/protocol/constant"
	pbchat "github.com/openimsdk/protocol/msg"
	"github.com/openimsdk/protocol/msggateway"
	pbpush "github.com/openimsdk/protocol/push"
	"github.com/openimsdk/protocol/sdkws"
	"github.com/openimsdk/tools/discovery"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/mcontext"
	"github.com/openimsdk/tools/mq/kafka"
	"github.com/openimsdk/tools/mq/memamq"
	"github.com/openimsdk/tools/utils/datautil"
	"github.com/openimsdk/tools/utils/jsonutil"
	"github.com/openimsdk/tools/utils/timeutil"
	"github.com/redis/go-redis/v9"
	"google.golang.org/protobuf/proto"
	"math/rand"
	"os"
	"strconv"
	"sync/atomic"
	"time"
)

type ConsumerHandler struct {
	pushConsumerGroup      *kafka.MConsumerGroup
	offlinePusher          offlinepush.OfflinePusher
	onlinePusher           OnlinePusher
	onlineCache            *rpccache.OnlineCache
	groupLocalCache        *rpccache.GroupLocalCache
	conversationLocalCache *rpccache.ConversationLocalCache
	msgRpcClient           rpcclient.MessageRpcClient
	conversationRpcClient  rpcclient.ConversationRpcClient
	groupRpcClient         rpcclient.GroupRpcClient
	webhookClient          *webhook.Client
	config                 *Config
	readCh                 chan *sdkws.MarkAsReadTips
}

func NewConsumerHandler(config *Config, offlinePusher offlinepush.OfflinePusher, rdb redis.UniversalClient,
	client discovery.SvcDiscoveryRegistry) (*ConsumerHandler, error) {
	var consumerHandler ConsumerHandler
	var err error
	consumerHandler.pushConsumerGroup, err = kafka.NewMConsumerGroup(config.KafkaConfig.Build(), config.KafkaConfig.ToPushGroupID,
		[]string{config.KafkaConfig.ToPushTopic}, true)
	if err != nil {
		return nil, err
	}
	userRpcClient := rpcclient.NewUserRpcClient(client, config.Share.RpcRegisterName.User, config.Share.IMAdminUserID)
	consumerHandler.offlinePusher = offlinePusher
	consumerHandler.onlinePusher = NewOnlinePusher(client, config)
	consumerHandler.groupRpcClient = rpcclient.NewGroupRpcClient(client, config.Share.RpcRegisterName.Group)
	consumerHandler.groupLocalCache = rpccache.NewGroupLocalCache(consumerHandler.groupRpcClient, &config.LocalCacheConfig, rdb)
	consumerHandler.msgRpcClient = rpcclient.NewMessageRpcClient(client, config.Share.RpcRegisterName.Msg)
	consumerHandler.conversationRpcClient = rpcclient.NewConversationRpcClient(client, config.Share.RpcRegisterName.Conversation)
	consumerHandler.conversationLocalCache = rpccache.NewConversationLocalCache(consumerHandler.conversationRpcClient, &config.LocalCacheConfig, rdb)
	consumerHandler.webhookClient = webhook.NewWebhookClient(config.WebhooksConfig.URL)
	consumerHandler.config = config
	consumerHandler.onlineCache = rpccache.NewOnlineCache(userRpcClient, consumerHandler.groupLocalCache, rdb, nil)
	consumerHandler.readCh = make(chan *sdkws.MarkAsReadTips, 1024*8)
	go consumerHandler.loopRead()
	return &consumerHandler, nil
}

func (c *ConsumerHandler) handleMs2PsChat(ctx context.Context, msg []byte) {
	msgFromMQ := pbchat.PushMsgDataToMQ{}
	if err := proto.Unmarshal(msg, &msgFromMQ); err != nil {
		log.ZError(ctx, "push Unmarshal msg err", err, "msg", string(msg))
		return
	}
	pbData := &pbpush.PushMsgReq{
		MsgData:        msgFromMQ.MsgData,
		ConversationID: msgFromMQ.ConversationID,
	}
	c.handlerConversationRead(ctx, pbData.MsgData)
	sec := msgFromMQ.MsgData.SendTime / 1000
	nowSec := timeutil.GetCurrentTimestampBySecond()
	log.ZDebug(ctx, "push msg", "msg", pbData.String(), "sec", sec, "nowSec", nowSec)
	if nowSec-sec > 10 {
		return
	}
	var err error
	switch msgFromMQ.MsgData.SessionType {
	case constant.ReadGroupChatType:
		err = c.Push2Group(ctx, pbData.MsgData.GroupID, pbData.MsgData)
	default:
		var pushUserIDList []string
		isSenderSync := datautil.GetSwitchFromOptions(pbData.MsgData.Options, constant.IsSenderSync)
		if !isSenderSync || pbData.MsgData.SendID == pbData.MsgData.RecvID {
			pushUserIDList = append(pushUserIDList, pbData.MsgData.RecvID)
		} else {
			pushUserIDList = append(pushUserIDList, pbData.MsgData.RecvID, pbData.MsgData.SendID)
		}
		err = c.Push2User(ctx, pushUserIDList, pbData.MsgData)
	}
	if err != nil {
		log.ZWarn(ctx, "push failed", err, "msg", pbData.String())
	}
}

func (*ConsumerHandler) Setup(sarama.ConsumerGroupSession) error { return nil }

func (*ConsumerHandler) Cleanup(sarama.ConsumerGroupSession) error { return nil }

func (c *ConsumerHandler) loopRead() {
	type markKey struct {
		ConversationID string
		UserID         string
	}
	type markSeq struct {
		ReadSeq int64
		MarkSeq int64
		Count   int64
	}
	type asyncRequest struct {
		ConversationID string
		UserID         string
		ReadSeq        int64
	}
	ctx := context.Background()
	ctx = mcontext.WithOpUserIDContext(ctx, c.config.Share.IMAdminUserID[0])
	opIDPrefix := fmt.Sprintf("mark_read_%d_%d_", os.Getpid(), rand.Uint32())
	var incr atomic.Uint64
	maxSeq := make(map[markKey]*markSeq, 1024*8)
	queue := memamq.NewMemoryQueue(32, 1024)
	defer queue.Stop()
	ticker := time.NewTicker(time.Second * 10)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			var markSeqs []asyncRequest
			for key, seq := range maxSeq {
				if seq.MarkSeq >= seq.ReadSeq {
					seq.Count++
					if seq.Count > 6 {
						delete(maxSeq, key)
					}
					continue
				}
				seq.Count = 0
				seq.MarkSeq = seq.ReadSeq
				markSeqs = append(markSeqs, asyncRequest{
					ConversationID: key.ConversationID,
					UserID:         key.UserID,
					ReadSeq:        seq.ReadSeq,
				})
			}
			if len(markSeqs) == 0 {
				continue
			}
			go func() {
				for i := range markSeqs {
					seq := markSeqs[i]
					_ = queue.PushCtx(ctx, func() {
						ctx = mcontext.SetOperationID(ctx, opIDPrefix+strconv.FormatUint(incr.Add(1), 10))
						_, err := c.msgRpcClient.Client.SetConversationHasReadSeq(ctx, &pbchat.SetConversationHasReadSeqReq{
							ConversationID: seq.ConversationID,
							UserID:         seq.UserID,
							HasReadSeq:     seq.ReadSeq,
							NoNotification: true,
						})
						if err != nil {
							log.ZError(ctx, "ConsumerHandler SetConversationHasReadSeq", err, "conversationID", seq.ConversationID, "userID", seq.UserID, "readSeq", seq.ReadSeq)
						}
					})
				}
			}()

		case tips, ok := <-c.readCh:
			if !ok {
				return
			}
			if tips.HasReadSeq <= 0 {
				continue
			}
			key := markKey{
				ConversationID: tips.ConversationID,
				UserID:         tips.MarkAsReadUserID,
			}
			ms, ok := maxSeq[key]
			if ok {
				if ms.ReadSeq < tips.HasReadSeq {
					ms.ReadSeq = tips.HasReadSeq
				}
			} else {
				ms = &markSeq{
					ReadSeq: tips.HasReadSeq,
					MarkSeq: 0,
				}
				maxSeq[key] = ms
			}
		}
	}
}

func (c *ConsumerHandler) ConsumeClaim(sess sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for msg := range claim.Messages() {
		ctx := c.pushConsumerGroup.GetContextFromMsg(msg)
		c.handleMs2PsChat(ctx, msg.Value)
		sess.MarkMessage(msg, "")
	}
	return nil
}

// Push2User Suitable for two types of conversations, one is SingleChatType and the other is NotificationChatType.
func (c *ConsumerHandler) Push2User(ctx context.Context, userIDs []string, msg *sdkws.MsgData) (err error) {
	log.ZDebug(ctx, "Get msg from msg_transfer And push msg", "userIDs", userIDs, "msg", msg.String())
	if err := c.webhookBeforeOnlinePush(ctx, &c.config.WebhooksConfig.BeforeOnlinePush, userIDs, msg); err != nil {
		return err
	}
	wsResults, err := c.GetConnsAndOnlinePush(ctx, msg, userIDs)
	if err != nil {
		return err
	}

	log.ZDebug(ctx, "single and notification push result", "result", wsResults, "msg", msg, "push_to_userID", userIDs)

	if !c.shouldPushOffline(ctx, msg) {
		return nil
	}

	for _, v := range wsResults {
		//message sender do not need offline push
		if msg.SendID == v.UserID {
			continue
		}
		//receiver online push success
		if v.OnlinePush {
			return nil
		}
	}
	offlinePushUserID := []string{msg.RecvID}

	//receiver offline push
	if err = c.webhookBeforeOfflinePush(ctx, &c.config.WebhooksConfig.BeforeOfflinePush,
		offlinePushUserID, msg, nil); err != nil {
		return err
	}

	err = c.offlinePushMsg(ctx, msg, offlinePushUserID)
	if err != nil {
		log.ZWarn(ctx, "offlinePushMsg failed", err, "offlinePushUserID", offlinePushUserID, "msg", msg)
		return nil
	}

	return nil
}

func (c *ConsumerHandler) shouldPushOffline(_ context.Context, msg *sdkws.MsgData) bool {
	isOfflinePush := datautil.GetSwitchFromOptions(msg.Options, constant.IsOfflinePush)
	if !isOfflinePush {
		return false
	}
	if msg.ContentType == constant.SignalingNotification {
		return false
	}
	return true
}

func (c *ConsumerHandler) GetConnsAndOnlinePush(ctx context.Context, msg *sdkws.MsgData, pushToUserIDs []string) ([]*msggateway.SingleMsgToUserResults, error) {
	var (
		onlineUserIDs  []string
		offlineUserIDs []string
	)
	for _, userID := range pushToUserIDs {
		online, err := c.onlineCache.GetUserOnline(ctx, userID)
		if err != nil {
			return nil, err
		}
		if online {
			onlineUserIDs = append(onlineUserIDs, userID)
		} else {
			offlineUserIDs = append(offlineUserIDs, userID)
		}
	}
	log.ZDebug(ctx, "GetConnsAndOnlinePush online cache", "sendID", msg.SendID, "recvID", msg.RecvID, "groupID", msg.GroupID, "sessionType", msg.SessionType, "clientMsgID", msg.ClientMsgID, "serverMsgID", msg.ServerMsgID, "offlineUserIDs", offlineUserIDs, "onlineUserIDs", onlineUserIDs)
	var result []*msggateway.SingleMsgToUserResults
	if len(onlineUserIDs) > 0 {
		var err error
		result, err = c.onlinePusher.GetConnsAndOnlinePush(ctx, msg, onlineUserIDs)
		if err != nil {
			return nil, err
		}
	}
	for _, userID := range offlineUserIDs {
		result = append(result, &msggateway.SingleMsgToUserResults{
			UserID: userID,
		})
	}
	return result, nil
}

func (c *ConsumerHandler) handlerConversationRead(ctx context.Context, msg *sdkws.MsgData) {
	if msg.ContentType != constant.HasReadReceipt {
		return
	}
	var elem sdkws.NotificationElem
	if err := json.Unmarshal(msg.Content, &elem); err != nil {
		log.ZError(ctx, "handlerConversationRead Unmarshal NotificationElem msg err", err, "msg", msg)
		return
	}
	var tips sdkws.MarkAsReadTips
	if err := json.Unmarshal([]byte(elem.Detail), &tips); err != nil {
		log.ZError(ctx, "handlerConversationRead Unmarshal MarkAsReadTips msg err", err, "msg", msg)
		return
	}
	if len(tips.Seqs) > 0 {
		for _, seq := range tips.Seqs {
			if tips.HasReadSeq < seq {
				tips.HasReadSeq = seq
			}
		}
		clear(tips.Seqs)
		tips.Seqs = nil
	}
	if tips.HasReadSeq < 0 {
		return
	}
	select {
	case c.readCh <- &tips:
	default:
		log.ZWarn(ctx, "handlerConversationRead readCh is full", nil, "markAsReadTips", &tips)
	}
}

func (c *ConsumerHandler) Push2Group(ctx context.Context, groupID string, msg *sdkws.MsgData) (err error) {
	log.ZDebug(ctx, "Get group msg from msg_transfer and push msg", "msg", msg.String(), "groupID", groupID)
	var pushToUserIDs []string
	if err = c.webhookBeforeGroupOnlinePush(ctx, &c.config.WebhooksConfig.BeforeGroupOnlinePush, groupID, msg,
		&pushToUserIDs); err != nil {
		return err
	}

	err = c.groupMessagesHandler(ctx, groupID, &pushToUserIDs, msg)
	if err != nil {
		return err
	}

	wsResults, err := c.GetConnsAndOnlinePush(ctx, msg, pushToUserIDs)
	if err != nil {
		return err
	}

	log.ZDebug(ctx, "group push result", "result", wsResults, "msg", msg)

	if !c.shouldPushOffline(ctx, msg) {
		return nil
	}
	needOfflinePushUserIDs := c.onlinePusher.GetOnlinePushFailedUserIDs(ctx, msg, wsResults, &pushToUserIDs)

	//filter some user, like don not disturb or don't need offline push etc.
	needOfflinePushUserIDs, err = c.filterGroupMessageOfflinePush(ctx, groupID, msg, needOfflinePushUserIDs)
	if err != nil {
		return err
	}
	// Use offline push messaging
	if len(needOfflinePushUserIDs) > 0 {
		var offlinePushUserIDs []string
		err = c.webhookBeforeOfflinePush(ctx, &c.config.WebhooksConfig.BeforeOfflinePush, needOfflinePushUserIDs, msg, &offlinePushUserIDs)
		if err != nil {
			return err
		}

		if len(offlinePushUserIDs) > 0 {
			needOfflinePushUserIDs = offlinePushUserIDs
		}

		err = c.offlinePushMsg(ctx, msg, needOfflinePushUserIDs)
		if err != nil {
			log.ZWarn(ctx, "offlinePushMsg failed", err, "groupID", groupID, "msg", msg)
			return nil
		}

	}

	return nil
}
func (c *ConsumerHandler) groupMessagesHandler(ctx context.Context, groupID string, pushToUserIDs *[]string, msg *sdkws.MsgData) (err error) {
	if len(*pushToUserIDs) == 0 {
		*pushToUserIDs, err = c.groupLocalCache.GetGroupMemberIDs(ctx, groupID)
		if err != nil {
			return err
		}
		switch msg.ContentType {
		case constant.MemberQuitNotification:
			var tips sdkws.MemberQuitTips
			if unmarshalNotificationElem(msg.Content, &tips) != nil {
				return err
			}
			if err = c.DeleteMemberAndSetConversationSeq(ctx, groupID, []string{tips.QuitUser.UserID}); err != nil {
				log.ZError(ctx, "MemberQuitNotification DeleteMemberAndSetConversationSeq", err, "groupID", groupID, "userID", tips.QuitUser.UserID)
			}
			*pushToUserIDs = append(*pushToUserIDs, tips.QuitUser.UserID)
		case constant.MemberKickedNotification:
			var tips sdkws.MemberKickedTips
			if unmarshalNotificationElem(msg.Content, &tips) != nil {
				return err
			}
			kickedUsers := datautil.Slice(tips.KickedUserList, func(e *sdkws.GroupMemberFullInfo) string { return e.UserID })
			if err = c.DeleteMemberAndSetConversationSeq(ctx, groupID, kickedUsers); err != nil {
				log.ZError(ctx, "MemberKickedNotification DeleteMemberAndSetConversationSeq", err, "groupID", groupID, "userIDs", kickedUsers)
			}

			*pushToUserIDs = append(*pushToUserIDs, kickedUsers...)
		case constant.GroupDismissedNotification:
			if msgprocessor.IsNotification(msgprocessor.GetConversationIDByMsg(msg)) {
				var tips sdkws.GroupDismissedTips
				if unmarshalNotificationElem(msg.Content, &tips) != nil {
					return err
				}
				log.ZInfo(ctx, "GroupDismissedNotificationInfo****", "groupID", groupID, "num", len(*pushToUserIDs), "list", pushToUserIDs)
				if len(c.config.Share.IMAdminUserID) > 0 {
					ctx = mcontext.WithOpUserIDContext(ctx, c.config.Share.IMAdminUserID[0])
				}
				defer func(groupID string) {
					if err = c.groupRpcClient.DismissGroup(ctx, groupID); err != nil {
						log.ZError(ctx, "DismissGroup Notification clear members", err, "groupID", groupID)
					}
				}(groupID)
			}
		}
	}
	return err
}

func (c *ConsumerHandler) offlinePushMsg(ctx context.Context, msg *sdkws.MsgData, offlinePushUserIDs []string) error {
	title, content, opts, err := c.getOfflinePushInfos(msg)
	if err != nil {
		return err
	}
	err = c.offlinePusher.Push(ctx, offlinePushUserIDs, title, content, opts)
	if err != nil {
		prommetrics.MsgOfflinePushFailedCounter.Inc()
		return err
	}
	return nil
}

func (c *ConsumerHandler) filterGroupMessageOfflinePush(ctx context.Context, groupID string, msg *sdkws.MsgData,
	offlinePushUserIDs []string) (userIDs []string, err error) {

	//todo local cache Obtain the difference set through local comparison.
	needOfflinePushUserIDs, err := c.conversationRpcClient.GetConversationOfflinePushUserIDs(
		ctx, conversationutil.GenGroupConversationID(groupID), offlinePushUserIDs)
	if err != nil {
		return nil, err
	}
	return needOfflinePushUserIDs, nil
}

func (c *ConsumerHandler) getOfflinePushInfos(msg *sdkws.MsgData) (title, content string, opts *options.Opts, err error) {
	type AtTextElem struct {
		Text       string   `json:"text,omitempty"`
		AtUserList []string `json:"atUserList,omitempty"`
		IsAtSelf   bool     `json:"isAtSelf"`
	}

	opts = &options.Opts{Signal: &options.Signal{}}
	if msg.OfflinePushInfo != nil {
		opts.IOSBadgeCount = msg.OfflinePushInfo.IOSBadgeCount
		opts.IOSPushSound = msg.OfflinePushInfo.IOSPushSound
		opts.Ex = msg.OfflinePushInfo.Ex
	}

	if msg.OfflinePushInfo != nil {
		title = msg.OfflinePushInfo.Title
		content = msg.OfflinePushInfo.Desc
	}
	if title == "" {
		switch msg.ContentType {
		case constant.Text:
			fallthrough
		case constant.Picture:
			fallthrough
		case constant.Voice:
			fallthrough
		case constant.Video:
			fallthrough
		case constant.File:
			title = constant.ContentType2PushContent[int64(msg.ContentType)]
		case constant.AtText:
			ac := AtTextElem{}
			_ = jsonutil.JsonStringToStruct(string(msg.Content), &ac)
		case constant.SignalingNotification:
			title = constant.ContentType2PushContent[constant.SignalMsg]
		default:
			title = constant.ContentType2PushContent[constant.Common]
		}
	}
	if content == "" {
		content = title
	}
	return
}
func (c *ConsumerHandler) DeleteMemberAndSetConversationSeq(ctx context.Context, groupID string, userIDs []string) error {
	conversationID := msgprocessor.GetConversationIDBySessionType(constant.ReadGroupChatType, groupID)
	maxSeq, err := c.msgRpcClient.GetConversationMaxSeq(ctx, conversationID)
	if err != nil {
		return err
	}
	return c.conversationRpcClient.SetConversationMaxSeq(ctx, userIDs, conversationID, maxSeq)
}
func unmarshalNotificationElem(bytes []byte, t any) error {
	var notification sdkws.NotificationElem
	if err := json.Unmarshal(bytes, &notification); err != nil {
		return err
	}

	return json.Unmarshal([]byte(notification.Detail), t)
}
