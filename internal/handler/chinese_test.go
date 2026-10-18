package handler

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net/http"
	"os"
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

	created := readingJSONRequest(t, router, http.MethodPost, "/api/v1/chinese/weeks", `{"label":"Week 1","items":[{"character":"𠀀"}]}`)
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

type enrichReplyProvider struct {
	reply string
	fail  bool
}

func (enrichReplyProvider) Name() string         { return "enrich-test" }
func (enrichReplyProvider) SupportsVision() bool { return false }
func (enrichReplyProvider) GenerateWithVision(context.Context, ai.VisionRequest) (string, error) {
	return "", nil
}
func (p enrichReplyProvider) GenerateText(context.Context, ai.TextRequest) (string, error) {
	if p.fail {
		return "", errors.New("down")
	}
	return p.reply, nil
}

// postChineseWeek posts body to a fresh router backed by a provider that
// answers reply, and returns the response, the captured log and the week list.
func postChineseWeek(t *testing.T, reply, body string) (code int, logged, weeks string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	database, err := appdb.Open(filepath.Join(t.TempDir(), "chinese-enrich.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	router := gin.New()
	NewChinese(database, ai.NewChain([]ai.Provider{enrichReplyProvider{reply: reply}})).Register(router.Group("/api/v1"))

	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)
	created := readingJSONRequest(t, router, http.MethodPost, "/api/v1/chinese/weeks", body)
	list := readingJSONRequest(t, router, http.MethodGet, "/api/v1/chinese/weeks", "")
	return created.Code, buf.String(), list.Body.String()
}

const oneItemWeek = `{"label":"Week 1","items":[{"character":"给"}]}`

func TestChineseWeekEmptyAIReplyIsLoggedWithoutContent(t *testing.T) {
	code, logged, weeks := postChineseWeek(t, "", oneItemWeek)
	if code != http.StatusOK || !strings.Contains(weeks, "gěi") {
		t.Fatalf("status=%d weeks=%s", code, weeks)
	}
	if strings.Count(strings.TrimSpace(logged), "\n") != 0 || !strings.Contains(logged, "no_json") || !strings.Contains(logged, "enrich-test") {
		t.Fatalf("log=%q", logged)
	}
	if strings.Contains(logged, "给") {
		t.Fatalf("log holds request content: %q", logged)
	}
}

func TestChineseWeekAcceptsASingleObjectReply(t *testing.T) {
	code, _, weeks := postChineseWeek(t, `{"character":"给","pinyin":"gěi"}`, oneItemWeek)
	if code != http.StatusOK || !strings.Contains(weeks, "gěi") {
		t.Fatalf("status=%d weeks=%s", code, weeks)
	}
}

func TestChineseWeekAcceptsANumericTone(t *testing.T) {
	code, _, weeks := postChineseWeek(t, `[{"character":"给","pinyin":"gěi","tone":3}]`, oneItemWeek)
	if code != http.StatusOK || !strings.Contains(weeks, "gěi") {
		t.Fatalf("status=%d weeks=%s", code, weeks)
	}
}

func TestChineseWeekAcceptsAFencedReply(t *testing.T) {
	code, _, weeks := postChineseWeek(t, "```json\n[{\"character\":\"给\",\"pinyin\":\"gěi\",\"tone\":\"3\"}]\n```", oneItemWeek)
	if code != http.StatusOK || !strings.Contains(weeks, "gěi") {
		t.Fatalf("status=%d weeks=%s", code, weeks)
	}
}

func TestChineseWeekParseFailureLogsErrorAndLengthNotReply(t *testing.T) {
	reply := `[{"character":"给","pinyin":["gěi"]}]`
	code, logged, _ := postChineseWeek(t, reply, oneItemWeek)
	if code != http.StatusOK {
		t.Fatalf("status=%d", code)
	}
	if !strings.Contains(logged, "parse_error") || !strings.Contains(logged, "cannot unmarshal") || !strings.Contains(logged, "reply_len=39") {
		t.Fatalf("log=%q", logged)
	}
	if strings.Contains(logged, "给") || strings.Contains(logged, "gěi") {
		t.Fatalf("log holds reply content: %q", logged)
	}
}

func TestChineseWeekWithPinyinIgnoresTheProvider(t *testing.T) {
	code, _, weeks := postChineseWeek(t, "", `{"label":"Week 1","items":[{"character":"给","pinyin":"gěi"}]}`)
	if code != http.StatusOK || !strings.Contains(weeks, "Week 1") {
		t.Fatalf("status=%d weeks=%s", code, weeks)
	}
}

func TestChineseWeekParseFailureDoesNotLogNumericContent(t *testing.T) {
	const value = "123456789012345678901234567890"
	reply := `[{"character":"给","pinyin":"gěi","id":` + value + `}]`

	code, logged, _ := postChineseWeek(t, reply, oneItemWeek)
	if code != http.StatusOK {
		t.Fatalf("status=%d", code)
	}
	if strings.Contains(logged, value) {
		t.Fatalf("log contains reply content: %q", logged)
	}
}

func TestChineseWeekProviderErrorLogNamesTheProvider(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database, err := appdb.Open(filepath.Join(t.TempDir(), "chinese-down.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	router := gin.New()
	NewChinese(database, ai.NewChain([]ai.Provider{enrichReplyProvider{fail: true}})).Register(router.Group("/api/v1"))
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)
	created := readingJSONRequest(t, router, http.MethodPost, "/api/v1/chinese/weeks", oneItemWeek)
	if created.Code != http.StatusOK {
		t.Fatalf("status=%d", created.Code)
	}
	if !strings.Contains(buf.String(), "provider=enrich-test kind=provider_error") {
		t.Fatalf("log=%q", buf.String())
	}
}

// saveWeekWith posts body with a provider that answers reply (or fails), and
// returns the response code and the items saved in the database.
func saveWeekWith(t *testing.T, fail bool, reply, body string) (int, []model.ChineseItem) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	database, err := appdb.Open(filepath.Join(t.TempDir(), "chinese-local.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	router := gin.New()
	h := NewChinese(database, ai.NewChain([]ai.Provider{enrichReplyProvider{reply: reply, fail: fail}}))
	h.Register(router.Group("/api/v1"))
	log.SetOutput(&bytes.Buffer{})
	defer log.SetOutput(os.Stderr)
	created := readingJSONRequest(t, router, http.MethodPost, "/api/v1/chinese/weeks", body)
	var weeks int
	if err := database.QueryRow(`SELECT COUNT(*) FROM chinese_weeks`).Scan(&weeks); err != nil {
		t.Fatal(err)
	}
	if created.Code != http.StatusOK {
		if weeks != 0 {
			t.Fatalf("status=%d but a week was saved", created.Code)
		}
		return created.Code, nil
	}
	var weekID string
	if err := database.QueryRow(`SELECT week_id FROM chinese_weeks`).Scan(&weekID); err != nil {
		t.Fatalf("week row missing: %v", err)
	}
	return created.Code, h.loadItems(context.Background(), weekID)
}

func pinyinOf(items []model.ChineseItem) []string {
	out := []string{}
	for _, it := range items {
		out = append(out, it.Pinyin)
	}
	return out
}

const sixCharWeek = `{"label":"Week 1","items":[{"character":"黄"},{"character":"蓝"},{"character":"彩"},{"character":"虹"},{"character":"金"},{"character":"蛋"}]}`

func TestChineseWeekSavesLocalPinyinWhenTheAIFails(t *testing.T) {
	code, items := saveWeekWith(t, true, "", sixCharWeek)
	if code != http.StatusOK {
		t.Fatalf("status=%d", code)
	}
	want := []string{"huáng", "lán", "cǎi", "hóng", "jīn", "dàn"}
	got := pinyinOf(items)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("pinyin=%v want %v", got, want)
	}
	for _, it := range items {
		if it.Meaning != "" || len(it.Words) != 0 {
			t.Fatalf("extras not empty: %#v", it)
		}
	}
	if items[0].Tone != "2" {
		t.Fatalf("tone=%q", items[0].Tone)
	}
}

func TestChineseWeekKeepsTypedPinyin(t *testing.T) {
	_, items := saveWeekWith(t, false, `[{"character":"黄","pinyin":"wáng"}]`, `{"label":"W","items":[{"character":"黄","pinyin":"huang2"}]}`)
	if len(items) != 1 || items[0].Pinyin != "huang2" {
		t.Fatalf("items=%#v", items)
	}
}

func TestChineseWeekAIPinyinReplacesLocalForAWord(t *testing.T) {
	body := `{"label":"W","items":[{"character":"银行"}]}`
	_, items := saveWeekWith(t, false, `[{"character":"银行","pinyin":"yín háng","meaning":"bank"}]`, body)
	if len(items) != 1 || items[0].Pinyin != "yín háng" || items[0].Meaning != "bank" {
		t.Fatalf("items=%#v", items)
	}
	_, items = saveWeekWith(t, true, "", body)
	if len(items) != 1 || items[0].Pinyin != "yín xíng" {
		t.Fatalf("items=%#v", items)
	}
}

func TestChineseWeekSingleCharacterKeepsLocalPinyin(t *testing.T) {
	_, items := saveWeekWith(t, false, `[{"character":"行","pinyin":"háng","meaning":"row"}]`, `{"label":"W","items":[{"character":"行"}]}`)
	if len(items) != 1 || items[0].Pinyin != "xíng" || items[0].Meaning != "row" {
		t.Fatalf("items=%#v", items)
	}
}

func TestChineseWeekFullAIItemsAreSaved(t *testing.T) {
	reply := `[{"character":"黄","meaning":"yellow","words":["黄色"],"exampleSentence":"香蕉是黄色的。","pinyinHint":"h","shapeHint":"s"}]`
	_, items := saveWeekWith(t, false, reply, `{"label":"W","items":[{"character":"黄"}]}`)
	if len(items) != 1 || items[0].Pinyin != "huáng" || items[0].Meaning != "yellow" || len(items[0].Words) != 1 || items[0].PinyinHint != "h" || items[0].ShapeHint != "s" || items[0].ExampleSentence == "" {
		t.Fatalf("items=%#v", items)
	}
}
