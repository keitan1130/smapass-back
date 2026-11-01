package entity

import "testing"

func TestIMEIValidation(t *testing.T) {
	for _, input := range []string{"490154203237518", " 000000000094821 "} {
		if _, err := NormalizeIMEI(input); err != nil {
			t.Errorf("valid IMEI rejected: %q", input)
		}
	}
	for _, input := range []string{"4821", "000000000000000", "490154203237519", "49015420323751a", "４９０１５４２０３２３７５１８"} {
		if _, err := NormalizeIMEI(input); err == nil {
			t.Errorf("invalid IMEI accepted: %q", input)
		}
	}
}
