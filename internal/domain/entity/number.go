package entity

import (
	"errors"
	"strings"
	"unicode"
)

var ErrInvalidNumber = errors.New("invalid certificate number")

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
