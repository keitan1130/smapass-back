package entity

import (
	"errors"
	"github.com/google/uuid"
)

var ErrInvalidDeviceID = errors.New("valid device UUID required")

func NormalizeDeviceID(input string) (string, error) {
	id, err := uuid.Parse(input)
	if err != nil || id == uuid.Nil {
		return "", ErrInvalidDeviceID
	}
	return id.String(), nil
}
