package media

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gocloud.dev/gcerrors"

	"github.com/wisalgroup/labena/pkg/database"
)

// SetVisibility changes an owned ready asset without changing its ID. It moves
// the object across the public/ storage boundary before confirming success.
// Call outside SQL transactions. Once recorded, failed changes remain pending
// for Recover; old public URLs may remain usable until recovery removes the source.
func (s *Library) SetVisibility(ctx context.Context, owner Owner, id uuid.UUID, visibility Visibility) (*Asset, error) {
	if !visibility.Valid() {
		return nil, ErrInvalidVisibility
	}
	if !owner.Valid() || id == uuid.Nil {
		return nil, ErrAssetNotFound
	}
	if err := database.RequireNoTransaction(ctx); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	asset, err := s.Get(ctx, owner, id)
	if err != nil {
		return nil, err
	}
	if s.disks[asset.Disk] == nil {
		return nil, ErrUnavailable
	}
	asset, err = s.repo.BeginVisibilityChange(ctx, owner, id, visibility)
	if err != nil {
		return nil, err
	}
	if asset.Status == AssetReady {
		return asset, nil
	}
	updated, err := s.finishVisibilityChange(ctx, asset)
	if err != nil {
		return nil, errors.Join(ErrVisibilityPending, fmt.Errorf("media: change visibility: %w", err))
	}
	return updated, nil
}

// Recovery retries the same move, including a crash after source removal but
// before SQL completion. Asset bytes are immutable, so a surviving destination
// belongs to this transition. Never mark a missing or incomplete object ready.
func (s *Library) finishVisibilityChange(ctx context.Context, asset *Asset) (*Asset, error) {
	bucket := s.disks[asset.Disk]
	if bucket == nil {
		return nil, ErrUnavailable
	}
	key := strings.TrimPrefix(asset.Key, "public/")
	if asset.VisibilityTarget == Public {
		key = "public/" + key
	}
	if key != asset.Key {
		if err := bucket.Copy(ctx, key, asset.Key, nil); err != nil {
			if gcerrors.Code(err) != gcerrors.NotFound {
				return nil, err
			}
			// NotFound may refer to a missing source, bucket or destination parent.
			// Only a complete surviving destination permits finalization.
			attrs, attrErr := bucket.Attributes(ctx, key)
			if attrErr != nil {
				return nil, errors.Join(err, attrErr)
			}
			if attrs.Size != asset.Size {
				return nil, fmt.Errorf("media: visibility destination size mismatch")
			}
		}
		if err := bucket.Delete(ctx, asset.Key); err != nil && gcerrors.Code(err) != gcerrors.NotFound {
			return nil, err
		}
	}
	return s.repo.CompleteVisibilityChange(ctx, asset, key)
}
