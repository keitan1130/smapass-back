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
	expected := &entity.Certificate{Number: "01ABCDEFGH"}
	repo.EXPECT().FindByNumber(ctx, "01ABCDEFGH").Return(expected, nil)
	actual, err := NewCertificateUsecase(repo).Get(ctx, " olabc-defgh ")
	if err != nil || actual != expected {
		t.Fatalf("got %+v, %v", actual, err)
	}
}

func TestCertificateListDeduplicatesAndRejectsInvalidInput(t *testing.T) {
	repo := mock.NewMockCertificateRepository(gomock.NewController(t))
	ctx := context.Background()
	expected := []entity.Certificate{{Number: "01ABCDEFGH"}}
	repo.EXPECT().FindByNumbers(ctx, []string{"01ABCDEFGH"}).Return(expected, nil)
	u := NewCertificateUsecase(repo)
	actual, err := u.List(ctx, []string{"01ABC-DEFGH", "olabcdefgh"})
	if err != nil || !reflect.DeepEqual(actual, expected) {
		t.Fatalf("got %+v, %v", actual, err)
	}
	// These inputs must never reach the repository.
	for _, input := range [][]string{nil, {"invalid"}, make([]string, 51)} {
		if _, err := u.List(ctx, input); !errors.Is(err, entity.ErrInvalidNumber) {
			t.Fatalf("input accepted: %v", input)
		}
	}
}

func TestCertificateRecordsDoesNotQueryMissingCertificate(t *testing.T) {
	repo := mock.NewMockCertificateRepository(gomock.NewController(t))
	ctx := context.Background()
	repo.EXPECT().FindByNumber(ctx, "01ABCDEFGH").Return(nil, repository.ErrNotFound)
	_, _, err := NewCertificateUsecase(repo).Records(ctx, "01ABCDEFGH", 100, 0)
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("got %v", err)
	}
}
