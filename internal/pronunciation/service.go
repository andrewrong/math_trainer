package pronunciation

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"mathtrainer/internal/googletts"
	"mathtrainer/internal/model"
)

const britishEnglish = "en-GB"

// Service owns local UK-IPA records and Google speech audio. Keeping the
// two capabilities together makes their provenance explicit without tying the
// learning UI to a third-party dictionary or audio host.
type Service struct {
	db         *sql.DB
	voice      string
	tts        *googletts.Client
	httpClient *http.Client
	cacheDir   string
	mu         sync.Mutex
}

func New(db *sql.DB) *Service {
	return &Service{
		db:         db,
		voice:      "en-GB-Chirp3-HD-Aoede",
		tts:        googletts.Default(),
		httpClient: &http.Client{Timeout: 6 * time.Second},
		cacheDir:   filepath.Join("web", "audio", "tts", "english"),
	}
}

func normalizeWord(word string) string {
	word = strings.ToLower(strings.TrimSpace(word))
	if word == "" || len(word) > 80 {
		return ""
	}
	for _, r := range word {
		if !unicode.IsLetter(r) && r != '\'' && r != '-' && r != ' ' {
			return ""
		}
	}
	return word
}

func (s *Service) LookupOrGenerate(ctx context.Context, words []string) ([]model.EnglishPronunciation, error) {
	unique := make([]string, 0, len(words))
	seen := map[string]bool{}
	for _, word := range words {
		if key := normalizeWord(word); key != "" && !seen[key] {
			seen[key] = true
			unique = append(unique, key)
		}
	}
	records := make(map[string]model.EnglishPronunciation, len(unique))
	for _, word := range unique {
		if record, ok, err := s.lookup(ctx, word); err != nil {
			return nil, err
		} else if ok && record.Verified {
			records[word] = record
		}
	}
	for _, word := range unique {
		if _, ok := records[word]; ok {
			continue
		}
		ipa, err := s.fetchYoudaoBritishIPA(ctx, word)
		if err != nil || ipa == "" {
			continue
		}
		record := model.EnglishPronunciation{Word: word, Locale: britishEnglish, IPA: ipa, Source: "youdao", Verified: false}
		if err := s.upsertDictionaryIPA(ctx, record); err != nil {
			return nil, err
		}
		if stored, ok, err := s.lookup(ctx, word); err != nil {
			return nil, err
		} else if ok {
			records[word] = stored
		}
	}
	out := make([]model.EnglishPronunciation, 0, len(unique))
	for _, word := range unique {
		if record, ok := records[word]; ok {
			out = append(out, record)
		}
	}
	return out, nil
}

func (s *Service) SaveParentVerified(ctx context.Context, word, ipa string) (model.EnglishPronunciation, error) {
	word, ipa = normalizeWord(word), strings.TrimSpace(ipa)
	if word == "" || ipa == "" || len([]rune(ipa)) > 80 {
		return model.EnglishPronunciation{}, fmt.Errorf("a word and an IPA value of up to 80 characters are required")
	}
	record := model.EnglishPronunciation{Word: word, Locale: britishEnglish, IPA: ipa, Source: "parent_verified", Verified: true}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO english_pronunciations(word_key,locale,ipa,source,verified,updated_at)
		VALUES(?,?,?,?,?,CURRENT_TIMESTAMP)
		ON CONFLICT(word_key,locale) DO UPDATE SET ipa=excluded.ipa,source=excluded.source,verified=excluded.verified,updated_at=CURRENT_TIMESTAMP`,
		record.Word, record.Locale, record.IPA, record.Source, 1)
	return record, err
}

func (s *Service) lookup(ctx context.Context, word string) (model.EnglishPronunciation, bool, error) {
	var record model.EnglishPronunciation
	var verified int
	err := s.db.QueryRowContext(ctx, `SELECT word_key,locale,ipa,source,verified FROM english_pronunciations WHERE word_key=? AND locale=?`, word, britishEnglish).
		Scan(&record.Word, &record.Locale, &record.IPA, &record.Source, &verified)
	if err == sql.ErrNoRows {
		return model.EnglishPronunciation{}, false, nil
	}
	if err != nil {
		return model.EnglishPronunciation{}, false, err
	}
	record.Verified = verified == 1
	return record, true, nil
}

func (s *Service) upsertDictionaryIPA(ctx context.Context, record model.EnglishPronunciation) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO english_pronunciations(word_key,locale,ipa,source,verified)
		VALUES(?,?,?,?,0)
		ON CONFLICT(word_key,locale) DO UPDATE SET ipa=excluded.ipa,source=excluded.source,verified=0,updated_at=CURRENT_TIMESTAMP
		WHERE english_pronunciations.verified=0`, record.Word, record.Locale, record.IPA, record.Source)
	return err
}

var youdaoUKIPA = regexp.MustCompile(`英</span><span class="phonetic"[^>]*>/\s*([^<]+?)\s*/`)

// fetchYoudaoBritishIPA reads the UK pronunciation displayed by Youdao's word
// page. It is deliberately best-effort: a network or markup failure leaves the
// card without IPA instead of displaying a machine phonetic alphabet.
func (s *Service) fetchYoudaoBritishIPA(ctx context.Context, word string) (string, error) {
	endpoint := "https://www.youdao.com/result?word=" + url.QueryEscape(word) + "&lang=en"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "LearningCompass/1.0")
	response, err := s.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Youdao returned %s", response.Status)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return "", err
	}
	match := youdaoUKIPA.FindStringSubmatch(string(body))
	if len(match) != 2 {
		return "", fmt.Errorf("Youdao UK IPA not found")
	}
	return strings.TrimSpace(match[1]), nil
}

// SynthesizeBritishSpeech returns an en-GB MP3 made by Google Text-to-Speech,
// from the cache when the same text was synthesized before.
func (s *Service) SynthesizeBritishSpeech(ctx context.Context, text string) (url string, cached bool, err error) {
	text = strings.TrimSpace(text)
	if text == "" || len([]rune(text)) > 200 {
		return "", false, fmt.Errorf("text must be 1-200 characters")
	}
	for _, r := range text {
		if !unicode.IsLetter(r) && !unicode.IsSpace(r) && r != '\'' && r != '-' && r != ',' && r != '.' && r != '!' && r != '?' {
			return "", false, fmt.Errorf("text contains unsupported characters")
		}
	}
	hash := sha256.Sum256([]byte("google|" + britishEnglish + "|" + s.voice + "|" + strings.ToLower(text)))
	name := hex.EncodeToString(hash[:]) + ".mp3"
	path := filepath.Join(s.cacheDir, name)
	url = "/audio/tts/english/" + name
	if _, statErr := os.Stat(path); statErr == nil {
		return url, true, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, statErr := os.Stat(path); statErr == nil {
		return url, true, nil
	}
	audio, err := s.tts.Synthesize(ctx, text, googletts.Voice{LanguageCode: britishEnglish, Name: s.voice})
	if err != nil {
		return "", false, err
	}
	if err := os.MkdirAll(s.cacheDir, 0755); err != nil {
		return "", false, err
	}
	tmp, err := os.CreateTemp(s.cacheDir, ".english-*.tmp")
	if err != nil {
		return "", false, err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(audio); err != nil {
		tmp.Close()
		return "", false, err
	}
	if err := tmp.Close(); err != nil {
		return "", false, err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return "", false, err
	}
	return url, false, nil
}
