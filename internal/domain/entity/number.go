package entity

import (
	"errors"
	"github.com/google/uuid"
	"strings"
	"unicode"
)

var ErrInvalidNumber = errors.New("invalid certificate number")

func NormalizeCertificateReference(input string) (string, error) {
	value := strings.TrimSpace(input)
	if len(value) == 36 {
		id, err := uuid.Parse(value)
		if err == nil {
			return id.String(), nil
		}
	}
	return NormalizeNumber(input)
}

// NormalizeNumber accepts Crockford Base32 input and returns ten storage characters.
func NormalizeNumber(input string) (string, error) {
	var b strings.Builder
	for _, r := range strings.ToUpper(input) {
		if r == '-' || unicode.IsSpace(r) {
			continue
		}
		switch r {
		case 'O':
			r = '0'
		case 'I', 'L':
			r = '1'
		}
		if !strings.ContainsRune("0123456789ABCDEFGHJKMNPQRSTVWXYZ", r) {
			return "", ErrInvalidNumber
		}
		b.WriteRune(r)
	}
	if b.Len() != 10 {
		return "", ErrInvalidNumber
	}
	return b.String(), nil
}
