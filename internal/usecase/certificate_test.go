package usecase

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"go.uber.org/mock/gomock"
	"smapass-back/internal/domain/entity"
	"smapass-back/internal/domain/repository"
	"smapass-back/internal/domain/repository/mock"
)

func TestCertificateGetNormalizesBeforeQuery(t *testing.T) {
	repo := mock.NewMockCertificateRepository(gomock.NewController(t))
	ctx := context.Background()
	expected := &entity.Certificate{IMEI: "490154203237518"}
	repo.EXPECT().FindLatestByIMEI(ctx, "490154203237518").Return(expected, nil)
	actual, err := NewCertificateUsecase(repo).Get(ctx, " 490154203237518 ")
	if err != nil || actual != expected {
		t.Fatalf("got %+v, %v", actual, err)
	}
}

func TestCertificateListDeduplicatesAndRejectsInvalidInput(t *testing.T) {
	repo := mock.NewMockCertificateRepository(gomock.NewController(t))
	ctx := context.Background()
	expected := []entity.Certificate{{IMEI: "490154203237518"}}
	repo.EXPECT().FindLatestByIMEIs(ctx, []string{"490154203237518"}).Return(expected, nil)
	u := NewCertificateUsecase(repo)
	actual, err := u.List(ctx, []string{"490154203237518", " 490154203237518 "})
	if err != nil || !reflect.DeepEqual(actual, expected) {
		t.Fatalf("got %+v, %v", actual, err)
	}
	// These inputs must never reach the repository.
	for _, input := range [][]string{nil, {"invalid"}, make([]string, 51)} {
		if _, err := u.List(ctx, input); !errors.Is(err, entity.ErrInvalidDevice) {
			t.Fatalf("input accepted: %v", input)
		}
	}
}

func TestCertificateRecordsChecksIMEIWithoutRequiringCertificate(t *testing.T) {
	repo := mock.NewMockCertificateRepository(gomock.NewController(t))
	ctx := context.Background()
	repo.EXPECT().Records(ctx, "490154203237518", 100, 0).Return(nil, 0, repository.ErrNotFound)
	_, _, err := NewCertificateUsecase(repo).Records(ctx, "490154203237518", 100, 0)
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("got %v", err)
	}
}
