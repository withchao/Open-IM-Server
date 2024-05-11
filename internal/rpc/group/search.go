package group

import (
	"context"
	"github.com/openimsdk/open-im-server/v3/pkg/common/convert"
	relationtb "github.com/openimsdk/open-im-server/v3/pkg/common/db/table/relation"
	pbgroup "github.com/openimsdk/protocol/group"
	"github.com/openimsdk/protocol/sdkws"
	"github.com/openimsdk/tools/utils/datautil"
)

func (s *groupServer) SearchGroupMember(ctx context.Context, req *pbgroup.SearchGroupMemberReq) (*pbgroup.SearchGroupMemberResp, error) {
	total, members, err := s.db.SearchGroupMember(ctx, req.Keyword, req.GroupID, req.Position, req.Pagination)
	if err != nil {
		return nil, err
	}
	return &pbgroup.SearchGroupMemberResp{
		Total: total,
		Members: datautil.Slice(members, func(e *relationtb.GroupMemberModel) *sdkws.GroupMemberFullInfo {
			return convert.Db2PbGroupMember(e)
		}),
	}, nil
}
