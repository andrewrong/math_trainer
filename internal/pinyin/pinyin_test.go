package pinyin

import "testing"

func TestReadUsesTheFirstListedReading(t *testing.T) {
	tests := []struct {
		in, text, tone string
	}{
		{"黄", "huáng", "2"},
		{"蛋", "dàn", "4"},
		{"行", "xíng", "2"},
		{"银行", "yín xíng", "2 2"},
		{"彩", "cǎi", "3"},
		{"妈", "mā", "1"},
	}
	for _, tt := range tests {
		text, tone, ok := Read(tt.in)
		if !ok || text != tt.text || tone != tt.tone {
			t.Errorf("Read(%q) = %q, %q, %v; want %q, %q, true", tt.in, text, tone, ok, tt.text, tt.tone)
		}
	}
}

func TestReadFailsForACharacterWithoutAnEntry(t *testing.T) {
	for _, in := range []string{"", "a", "黄a", "\U00020000"} {
		if _, _, ok := Read(in); ok {
			t.Errorf("Read(%q) ok, want not ok", in)
		}
	}
}
