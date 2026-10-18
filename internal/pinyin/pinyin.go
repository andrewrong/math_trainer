// Package pinyin reads the pinyin of Chinese characters from an embedded copy
// of the data file of github.com/mozillazg/pinyin-data (MIT, see LICENSE).
package pinyin

import (
	_ "embed"
	"strings"
	"sync"
	"unicode/utf8"
)

//go:embed pinyin.txt
var data string

var (
	once     sync.Once
	readings map[rune]string
)

// toneMarks maps each toned vowel to its tone number.
var toneMarks = map[rune]byte{
	'ā': '1', 'ē': '1', 'ī': '1', 'ō': '1', 'ū': '1', 'ǖ': '1',
	'á': '2', 'é': '2', 'í': '2', 'ó': '2', 'ú': '2', 'ǘ': '2',
	'ǎ': '3', 'ě': '3', 'ǐ': '3', 'ǒ': '3', 'ǔ': '3', 'ǚ': '3',
	'à': '4', 'è': '4', 'ì': '4', 'ò': '4', 'ù': '4', 'ǜ': '4',
}

func load() {
	readings = map[rune]string{}
	for _, line := range strings.Split(data, "\n") {
		code, rest, ok := strings.Cut(line, ": ")
		if !ok || !strings.HasPrefix(code, "U+") {
			continue
		}
		var r rune
		for _, c := range code[2:] {
			switch {
			case c >= '0' && c <= '9':
				r = r<<4 | (c - '0')
			case c >= 'A' && c <= 'F':
				r = r<<4 | (c - 'A' + 10)
			}
		}
		list, _, _ := strings.Cut(rest, "#")
		first, _, _ := strings.Cut(strings.TrimSpace(list), ",")
		if first != "" {
			readings[r] = first
		}
	}
}

// Read returns the most common reading of every character of s with tone
// marks, separated by a space, and the tone numbers (1-4, 5 for neutral)
// separated by a space. ok is false when s is empty or holds a character
// without an entry.
func Read(s string) (text, tone string, ok bool) {
	once.Do(load)
	if s == "" || !utf8.ValidString(s) {
		return "", "", false
	}
	var syllables, tones []string
	for _, r := range s {
		syl, found := readings[r]
		if !found {
			return "", "", false
		}
		t := byte('5')
		for _, c := range syl {
			if n, marked := toneMarks[c]; marked {
				t = n
				break
			}
		}
		syllables = append(syllables, syl)
		tones = append(tones, string(t))
	}
	return strings.Join(syllables, " "), strings.Join(tones, " "), true
}
