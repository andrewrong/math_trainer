package handler

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	appdb "mathtrainer/internal/db"
)

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
