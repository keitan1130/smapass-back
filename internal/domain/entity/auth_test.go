package entity

import (
	"bytes"
	"testing"
)

func TestOpaqueCredentialsAreRandomAndSeparated(t *testing.T) {
	a, b := NewTokenMaterial(), NewTokenMaterial()
	if !ValidToken(a.Access, "at") || !ValidToken(a.Refresh, "rt") || ValidToken(a.Access, "rt") || ValidToken(a.Refresh, "at") {
		t.Fatal("token type or encoding is invalid")
	}
	if a.Access == b.Access || a.Refresh == b.Refresh || bytes.Equal(TokenDigest(a.Access), TokenDigest(a.Refresh)) {
		t.Fatal("credentials reused")
	}
	for _, v := range []string{"", "490154203237518", "Bearer " + a.Access, a.Access + "=", a.Access[:48] + "!"} {
		if ValidToken(v, "at") {
			t.Fatal("invalid credential accepted")
		}
	}
}
