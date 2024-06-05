package incrversion

import (
	"context"
	"fmt"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/utils/datautil"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

//func Limit(maxSync int, version uint64) int {
//	if version == 0 {
//		return 0
//	}
//	return maxSync
//}

const (
	tagQuery = iota + 1
	tagFull
	tageEqual
)

type Option[A, B any] struct {
	Ctx             context.Context
	VersionKey      string
	VersionID       string
	VersionNumber   uint64
	SyncLimit       int
	CacheMaxVersion func(ctx context.Context, dId string) (*model.VersionLog, error)
	Version         func(ctx context.Context, dId string, version uint, limit int) (*model.VersionLog, error)
	SortID          func(ctx context.Context, dId string) ([]string, error)
	Find            func(ctx context.Context, ids []string) ([]A, error)
	ID              func(elem A) string
	Resp            func(version *model.VersionLog, delIDs []string, list []A, full bool) *B
}

func (o *Option[A, B]) newError(msg string) error {
	return errs.ErrInternalServer.WrapMsg(msg)
}

func (o *Option[A, B]) check() error {
	if o.Ctx == nil {
		return o.newError("opt ctx is nil")
	}
	if o.VersionKey == "" {
		return o.newError("versionKey is empty")
	}
	if o.SyncLimit <= 0 {
		return o.newError("invalid synchronization quantity")
	}
	if o.Version == nil {
		return o.newError("func version is nil")
	}
	if o.SortID == nil {
		return o.newError("func allID is nil")
	}
	if o.Find == nil {
		return o.newError("func find is nil")
	}
	if o.ID == nil {
		return o.newError("func id is nil")
	}
	if o.Resp == nil {
		return o.newError("func resp is nil")
	}
	return nil
}

func (o *Option[A, B]) validVersion() bool {
	objID, err := primitive.ObjectIDFromHex(o.VersionID)
	return err == nil && (!objID.IsZero()) && o.VersionNumber > 0
}

func (o *Option[A, B]) equalID(objID primitive.ObjectID) bool {
	return o.VersionID == objID.Hex()
}

func (o *Option[A, B]) getVersion(tag *int) (*model.VersionLog, error) {
	if o.CacheMaxVersion == nil {
		if o.validVersion() {
			*tag = tagQuery
			return o.Version(o.Ctx, o.VersionKey, uint(o.VersionNumber), o.SyncLimit)
		}
		*tag = tagFull
		return o.Version(o.Ctx, o.VersionKey, 0, 0)
	} else {
		cache, err := o.CacheMaxVersion(o.Ctx, o.VersionKey)
		if err != nil {
			return nil, err
		}
		if !o.validVersion() {
			*tag = tagFull
			return cache, nil
		}
		if !o.equalID(cache.ID) {
			*tag = tagFull
			return cache, nil
		}
		if o.VersionNumber == uint64(cache.Version) {
			*tag = tageEqual
			return cache, nil
		}
		*tag = tagQuery
		return o.Version(o.Ctx, o.VersionKey, uint(o.VersionNumber), o.SyncLimit)
	}
}

func (o *Option[A, B]) Build() (*B, error) {
	if err := o.check(); err != nil {
		return nil, err
	}
	var tag int
	version, err := o.getVersion(&tag)
	if err != nil {
		return nil, err
	}
	var full bool
	switch tag {
	case tagQuery:
		full = version.ID.Hex() != o.VersionID || uint64(version.Version) < o.VersionNumber || len(version.Logs) != version.LogLen
	case tagFull:
		full = true
	case tageEqual:
		full = false
	default:
		panic(fmt.Errorf("undefined tag %d", tag))
	}
	var (
		deleteIDs []string
		changeIDs []string
	)
	//full := o.VersionID != version.ID.Hex() || version.Full()
	if full {
		changeIDs, err = o.SortID(o.Ctx, o.VersionKey)
		if err != nil {
			return nil, err
		}
	} else {
		deleteIDs, changeIDs = version.DeleteAndChangeIDs()
	}
	var list []A
	if len(changeIDs) > 0 {
		list, err = o.Find(o.Ctx, changeIDs)
		if err != nil {
			return nil, err
		}
		if (!full) && o.ID != nil && len(changeIDs) != len(list) {
			foundIDs := datautil.SliceSetAny(list, o.ID)
			for _, id := range changeIDs {
				if _, ok := foundIDs[id]; !ok {
					deleteIDs = append(deleteIDs, id)
				}
			}
		}
	}
	return o.Resp(version, deleteIDs, list, full), nil
}
