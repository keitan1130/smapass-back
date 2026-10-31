package entity

import "testing"

func TestNormalizeNumber(t *testing.T) {
	for _, input := range []string{"01ABCDEFGH", "01abc-defgh", "  olabc defgh\t", "0IABCDEFGH", "0LABCDEFGH"} {
		number, err := NormalizeNumber(input)
		if err != nil || number != "01ABCDEFGH" {
			t.Errorf("NormalizeNumber(%q) = %q, %v", input, number, err)
		}
	}
	for _, input := range []string{"", "01ABCDEFG", "01ABCDEFGHJ", "01ABCDEFＧH", "01ABCDE/..", "01ABCDEFUH", "01ABCDEF?H"} {
		if _, err := NormalizeNumber(input); err == nil {
			t.Errorf("accepted invalid number %q", input)
		}
	}
}
