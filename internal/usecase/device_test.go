package usecase

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"smapass-back/internal/domain/entity"
	"smapass-back/internal/domain/repository"
)

type readRepository struct {
	repository.DeviceRepository
	searched, listed []string
	readID           string
	limit, offset    int
}

func (r *readRepository) Search(_ context.Context, inputs []string) ([]entity.DeviceReference, error) {
	r.searched = inputs
	return []entity.DeviceReference{{IMEI: inputs[0], ID: "abcdefab-1234-4234-8234-123456789012"}}, nil
}
func (r *readRepository) ListStates(_ context.Context, inputs []string) ([]entity.DeviceSummary, error) {
	r.listed = inputs
	return []entity.DeviceSummary{}, nil
}
func (r *readRepository) FindState(_ context.Context, id string, limit, offset int) (*entity.DeviceState, error) {
	r.readID, r.limit, r.offset = id, limit, offset
	return &entity.DeviceState{ID: id}, nil
}
func TestSearchUsesIMEIAndDisplayUsesUUID(t *testing.T) {
	ctx := context.Background()
	repo := &readRepository{}
	u := NewDeviceUsecase(repo, nil)
	refs, err := u.Search(ctx, []string{"490154203237518", "490154203237518"})
	if err != nil || len(refs) != 1 || !reflect.DeepEqual(repo.searched, []string{"490154203237518"}) {
		t.Fatalf("search: %v %v", refs, err)
	}
	id := refs[0].ID
	if _, err = u.State(ctx, id, 2, 7); err != nil || repo.readID != id || repo.limit != 2 || repo.offset != 7 {
		t.Fatalf("display/page: %v", err)
	}
	if _, err = u.State(ctx, "490154203237518", 100, 0); !errors.Is(err, entity.ErrInvalidDeviceID) {
		t.Fatalf("display accepted IMEI: %v", err)
	}
	if _, err = u.Search(ctx, []string{id}); !errors.Is(err, entity.ErrInvalidDevice) {
		t.Fatalf("search accepted UUID: %v", err)
	}
	if _, err = u.List(ctx, []string{"ABCDEFAB-1234-4234-8234-123456789012", id}); err != nil || !reflect.DeepEqual(repo.listed, []string{id}) {
		t.Fatalf("UUID normalization/deduplication: %v %v", repo.listed, err)
	}
	for _, inputs := range [][]string{nil, {""}, {"bad"}, {"00000000-0000-0000-0000-000000000000"}, make([]string, 51)} {
		if _, err = u.List(ctx, inputs); !errors.Is(err, entity.ErrInvalidDeviceID) {
			t.Fatalf("invalid UUID batch accepted: %v", inputs)
		}
	}
	for _, inputs := range [][]string{nil, {""}, {"bad"}, {"490154203237518", ""}, make([]string, 51)} {
		if _, err = u.Search(ctx, inputs); !errors.Is(err, entity.ErrInvalidDevice) {
			t.Fatalf("invalid IMEI batch accepted: %v", inputs)
		}
	}
}
