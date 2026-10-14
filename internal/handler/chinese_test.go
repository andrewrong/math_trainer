package handler

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"mathtrainer/internal/ai"
	appdb "mathtrainer/internal/db"
	"mathtrainer/internal/model"
)

type chineseContextStoryProvider struct{}

func (chineseContextStoryProvider) Name() string         { return "chinese-context-story-test" }
func (chineseContextStoryProvider) SupportsVision() bool { return false }
func (chineseContextStoryProvider) GenerateWithVision(context.Context, ai.VisionRequest) (string, error) {
	return "", nil
}
func (chineseContextStoryProvider) GenerateText(_ context.Context, req ai.TextRequest) (string, error) {
	if strings.Contains(req.UserPrompt, "independent quality checker") {
		return `{"approved":true,"reason":"Simple everyday context."}`, nil
	}
	return `{"sentences":[{"text":"小明把书放进书包。","annotations":[]},{"text":"他在操场跑步。","annotations":[{"word":"操场","pinyin":"cāo chǎng"}]}]}`, nil
}

func TestWeakestChineseKindIncludesToneAndWriting(t *testing.T) {
	tests := []struct {
		name string
		stat chineseStat
		want string
	}{
		{name: "tone", stat: chineseStat{PinyinWrong: 2, ToneWrong: 4}, want: "声调"},
		{name: "writing", stat: chineseStat{RecognitionWrong: 3, WritingWrong: 5}, want: "写字"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := weakestChineseKind(tt.stat); got != tt.want {
				t.Fatalf("weakestChineseKind() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestChineseStaticReviewCatalogAppearsInWrongs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database, err := appdb.Open(filepath.Join(t.TempDir(), "chinese.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	router := gin.New()
	NewChinese(database, nil).Register(router.Group("/api/v1"))

	updated := readingJSONRequest(t, router, http.MethodPut, "/api/v1/chinese/review", `{"character":"房","kind":"writing","correct":false,"item":{"character":"房","pinyin":"fáng","meaning":"room; house","words":["房子"],"shapeHint":"户 over 方"}}`)
	if updated.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", updated.Code, updated.Body.String())
	}
	wrongs := readingJSONRequest(t, router, http.MethodGet, "/api/v1/chinese/wrongs", "")
	if wrongs.Code != http.StatusOK || !strings.Contains(wrongs.Body.String(), `"character":"房"`) || !strings.Contains(wrongs.Body.String(), `"weakKind":"写字"`) {
		t.Fatalf("wrongs status=%d body=%s", wrongs.Code, wrongs.Body.String())
	}
}

func TestChineseWeekRejectsItemsWithoutPronunciationMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database, err := appdb.Open(filepath.Join(t.TempDir(), "chinese-metadata.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	router := gin.New()
	NewChinese(database, nil).Register(router.Group("/api/v1"))

	created := readingJSONRequest(t, router, http.MethodPost, "/api/v1/chinese/weeks", `{"label":"Week 1","items":[{"character":"给"}]}`)
	if created.Code != http.StatusUnprocessableEntity {
		t.Fatalf("week with missing pinyin status=%d body=%s", created.Code, created.Body.String())
	}
	weeks := readingJSONRequest(t, router, http.MethodGet, "/api/v1/chinese/weeks", "")
	if strings.Contains(weeks.Body.String(), "Week 1") {
		t.Fatalf("incomplete week was persisted: %s", weeks.Body.String())
	}
}

func TestChineseContextStoryDerivesOneBlankPerWeeklyItem(t *testing.T) {
	items := []model.ChineseItem{{Character: "放"}, {Character: "跑"}}
	sentences := []model.ChineseContextSentence{
		{Text: "小明把书放进书包。"},
		{Text: "他在操场跑步。", Annotations: []model.ChinesePinyinAnnotation{{Word: "操场", Pinyin: "cāo chǎng"}}},
	}
	blanks, err := validateChineseContextStory(sentences, items)
	if err != nil {
		t.Fatal(err)
	}
	if len(blanks) != 2 || blanks[0].Sentence != "小明把书____进书包。" || blanks[1].Sentence != "他在操场____步。" {
		t.Fatalf("derived blanks = %#v", blanks)
	}
}

func TestChineseContextStoryRejectsOverlappingWeeklyItems(t *testing.T) {
	_, err := validateChineseContextStory(
		[]model.ChineseContextSentence{{Text: "小明放下书后跑回家。"}, {Text: "妈妈在门口等他。"}},
		[]model.ChineseItem{{Character: "放"}, {Character: "跑"}},
	)
	if err == nil || !strings.Contains(err.Error(), "more than one weekly item") {
		t.Fatalf("expected overlapping story to be rejected, got %v", err)
	}
}

func TestChineseContextStoryRejectsAChildUnfriendlyLongSentence(t *testing.T) {
	_, err := validateChineseContextStory(
		[]model.ChineseContextSentence{{Text: "放学以后小明高高兴兴地带着很多很多很重的书包慢慢走过安静的小路回家。"}},
		[]model.ChineseItem{{Character: "放"}},
	)
	if err == nil || !strings.Contains(err.Error(), "too long") {
		t.Fatalf("expected long sentence to be rejected, got %v", err)
	}
}

func TestChineseContextStoryEndpointPersistsACheckedStory(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database, err := appdb.Open(filepath.Join(t.TempDir(), "chinese-context.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	router := gin.New()
	NewChinese(database, ai.NewChain([]ai.Provider{chineseContextStoryProvider{}})).Register(router.Group("/api/v1"))
	created := readingJSONRequest(t, router, http.MethodPost, "/api/v1/chinese/weeks", `{"label":"Week 1","items":[{"character":"放","pinyin":"fàng"},{"character":"跑","pinyin":"pǎo"}]}`)
	if created.Code != http.StatusOK {
		t.Fatalf("create week status=%d body=%s", created.Code, created.Body.String())
	}
	story := readingJSONRequest(t, router, http.MethodPost, "/api/v1/chinese/context-stories/label:V2VlayAx", "")
	if story.Code != http.StatusOK || !strings.Contains(story.Body.String(), `"sentence":"小明把书____进书包。"`) {
		t.Fatalf("context story status=%d body=%s", story.Code, story.Body.String())
	}
	loaded := readingJSONRequest(t, router, http.MethodGet, "/api/v1/chinese/context-stories/label:V2VlayAx", "")
	if loaded.Code != http.StatusOK || !strings.Contains(loaded.Body.String(), `"word":"操场"`) {
		t.Fatalf("saved context story status=%d body=%s", loaded.Code, loaded.Body.String())
	}
}
