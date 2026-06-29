package handler

import (
	"testing"

	"github.com/gin-gonic/gin"

	"mathtrainer/internal/model"
)

func TestP4CurriculumPacksAreDeepAndTopicSpecific(t *testing.T) {
	codes := []string{
		"P4-WHOLE-100000",
		"P4-FACTORS-MULTIPLES",
		"P4-FOUR-OPERATIONS",
		"P4-FRACTIONS-MIXED",
		"P4-DECIMALS",
		"P4-AREA-PERIMETER",
		"P4-GEOMETRY-ANGLES",
		"P4-GEOMETRY-SYMMETRY",
		"P4-GEOMETRY-NETS",
		"P4-STATISTICS",
	}
	seenBigIdeas := map[string]string{}
	for _, code := range codes {
		pack := buildTopicLessonPack(model.CurriculumTopic{TopicCode: code})
		big, _ := pack["bigIdea"].(string)
		if big == "" || big == "Mathematics is a way to explain a situation, not just a way to get an answer." {
			t.Fatalf("%s has generic big idea: %q", code, big)
		}
		if prev := seenBigIdeas[big]; prev != "" {
			t.Fatalf("%s repeats big idea from %s: %q", code, prev, big)
		}
		seenBigIdeas[big] = code

		requireLen(t, code, pack, "methodSteps", 6)
		requireLen(t, code, pack, "workedExamples", 4)
		requireLen(t, code, pack, "commonMisconceptions", 3)
		requireLen(t, code, pack, "guidedPractice", 6)
		requireLen(t, code, pack, "miniTest", 4)
		requireLen(t, code, pack, "challenge", 1)

		guided := pack["guidedPractice"].([]gin.H)
		mini := pack["miniTest"].([]gin.H)
		if guided[0]["prompt"] == mini[0]["prompt"] {
			t.Fatalf("%s guided practice and mini test start with the same question", code)
		}
	}
}

func requireLen(t *testing.T, code string, pack map[string]any, key string, min int) {
	t.Helper()
	switch items := pack[key].(type) {
	case []gin.H:
		if len(items) < min {
			t.Fatalf("%s %s len=%d, want at least %d", code, key, len(items), min)
		}
	case []string:
		if len(items) < min {
			t.Fatalf("%s %s len=%d, want at least %d", code, key, len(items), min)
		}
	default:
		t.Fatalf("%s %s has unexpected type %T", code, key, pack[key])
	}
}
