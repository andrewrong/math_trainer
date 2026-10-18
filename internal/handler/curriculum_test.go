package handler

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
		"P4-TIME",
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
		if pack["artifactVersion"] != p4ArtifactVersion {
			t.Fatalf("%s artifactVersion=%v, want %s", code, pack["artifactVersion"], p4ArtifactVersion)
		}

		requireLen(t, code, pack, "methodSteps", 6)
		requireLen(t, code, pack, "workedExamples", 4)
		requireLen(t, code, pack, "commonMisconceptions", 3)
		requireLen(t, code, pack, "guidedPractice", 6)
		requireLen(t, code, pack, "miniTest", 4)
		requireLen(t, code, pack, "challenge", 1)
		requireP4Tabs(t, code, pack)
		requireNoDebugChoiceText(t, code, pack)

		guided := pack["guidedPractice"].([]gin.H)
		mini := pack["miniTest"].([]gin.H)
		if guided[0]["prompt"] == mini[0]["prompt"] {
			t.Fatalf("%s guided practice and mini test start with the same question", code)
		}
	}
}

func TestP5CurriculumPacksAreDeepAndTextbookAligned(t *testing.T) {
	codes := []string{
		"P5-WHOLE-OPERATIONS",
		"P5-FRACTIONS",
		"P5-AREA-VOLUME",
		"P5-RATIO",
		"P5-DECIMALS",
		"P5-PERCENTAGE-RATE",
		"P5-AVERAGE",
		"P5-GEOMETRY",
	}
	for _, code := range codes {
		pack := buildTopicLessonPack(model.CurriculumTopic{TopicCode: code})
		big, _ := pack["bigIdea"].(string)
		if big == "" || big == "Mathematics is a way to explain a situation, not just a way to get an answer." {
			t.Fatalf("%s has generic big idea: %q", code, big)
		}
		if pack["artifactVersion"] != p5ArtifactVersion {
			t.Fatalf("%s artifactVersion=%v, want %s", code, pack["artifactVersion"], p5ArtifactVersion)
		}
		requireLen(t, code, pack, "methodSteps", 6)
		requireLen(t, code, pack, "workedExamples", 4)
		requireLen(t, code, pack, "commonMisconceptions", 3)
		requireLen(t, code, pack, "guidedPractice", 6)
		requireLen(t, code, pack, "miniTest", 4)
		requireLen(t, code, pack, "challenge", 1)
		requireP4Tabs(t, code, pack)
		requireNoDebugChoiceText(t, code, pack)
	}
}

func TestP5SeedIncludesMyPals5A5BContents(t *testing.T) {
	topics := map[string]bool{}
	for _, topic := range curriculumSeedTopics() {
		if topic.Grade == "P5" {
			topics[topic.Code] = true
		}
	}
	for _, code := range []string{"P5-RATIO", "P5-AVERAGE", "P5-GEOMETRY"} {
		if !topics[code] {
			t.Fatalf("P5 seed missing %s", code)
		}
	}
	mappings := map[string]string{}
	for _, m := range curriculumSeedTextbookMappings() {
		if strings.HasPrefix(m.TopicCode, "P5-") {
			mappings[m.Book+"|"+m.ChapterTitle] = m.TopicCode
		}
	}
	want := map[string]string{
		"5A|Chapter 1 Lesson 1: Numbers to 10 Million":                     "P5-WHOLE-OPERATIONS",
		"5A|Chapter 7 Lesson 3: Comparing Three Quantities":                "P5-RATIO",
		"5A|Chapter 8 Lesson 3: Volume of a Cube and a Cuboid":             "P5-AREA-VOLUME",
		"5B|Chapter 10 Lesson 4: Percentage of a Quantity":                 "P5-PERCENTAGE-RATE",
		"5B|Chapter 11 Lesson 1: Understanding Average":                    "P5-AVERAGE",
		"5B|Chapter 15 Lesson 1: Parallelograms, Rhombuses and Trapeziums": "P5-GEOMETRY",
		"5B|Chapter 14 Lesson 2: Sum of Angles in a Triangle":              "P5-GEOMETRY",
		"5B|Chapter 9 Lesson 3: Converting Measurements":                   "P5-DECIMALS",
	}
	for key, code := range want {
		if got := mappings[key]; got != code {
			t.Fatalf("mapping %s=%q, want %q", key, got, code)
		}
	}
}

func TestP5PacksTeachCoreModels(t *testing.T) {
	cases := map[string][]string{
		"P5-WHOLE-OPERATIONS": {"Brackets say which group", "operation priority", "place movement"},
		"P5-FRACTIONS":        {"fraction can be a division statement", "Multiplying by a proper fraction makes the amount smaller"},
		"P5-RATIO":            {"Each ratio part is a unit", "bar models"},
		"P5-PERCENTAGE-RATE":  {"100% means the whole", "per one unit"},
		"P5-AREA-VOLUME":      {"triangle is half", "Volume counts unit cubes"},
		"P5-AVERAGE":          {"Average means equal sharing", "Total = average"},
		"P5-GEOMETRY":         {"Angles on a straight line add to 180", "Vertically opposite angles are equal"},
	}
	for code, wants := range cases {
		raw, err := json.Marshal(buildTopicLessonPack(model.CurriculumTopic{TopicCode: code}))
		if err != nil {
			t.Fatal(err)
		}
		text := string(raw)
		for _, want := range wants {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q", code, want)
			}
		}
	}
}

func TestP3MultiplicationDivisionBuildsDivisionSense(t *testing.T) {
	pack := buildTopicLessonPack(model.CurriculumTopic{TopicCode: "P3-MULT-DIV"})
	if pack["artifactVersion"] != p3ArtifactVersion {
		t.Fatalf("artifactVersion=%v, want %s", pack["artifactVersion"], p3ArtifactVersion)
	}
	requireLen(t, "P3-MULT-DIV", pack, "methodSteps", 8)
	requireLen(t, "P3-MULT-DIV", pack, "workedExamples", 6)
	requireLen(t, "P3-MULT-DIV", pack, "guidedPractice", 8)
	requireLen(t, "P3-MULT-DIV", pack, "miniTest", 8)
	raw, err := json.Marshal(pack)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{
		"sharing equally",
		"making groups",
		"missing multiplication",
		"remainder must be smaller than the divisor",
		"divisor × quotient + remainder",
		"Both division stories can use the same equation",
		"how many are in each group",
		"how many full groups can we make",
		"8 vans are needed",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("P3-MULT-DIV missing %q", want)
		}
	}
}

func TestP3AdditionSubtractionExplainsRegroupingByValue(t *testing.T) {
	pack := buildTopicLessonPack(model.CurriculumTopic{TopicCode: "P3-ADD-SUB"})
	if pack["artifactVersion"] != p3ArtifactVersion {
		t.Fatalf("artifactVersion=%v, want %s", pack["artifactVersion"], p3ArtifactVersion)
	}
	requireLen(t, "P3-ADD-SUB", pack, "methodSteps", 8)
	requireLen(t, "P3-ADD-SUB", pack, "workedExamples", 6)
	requireLen(t, "P3-ADD-SUB", pack, "guidedPractice", 8)
	requireLen(t, "P3-ADD-SUB", pack, "miniTest", 8)
	raw, err := json.Marshal(pack)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{
		"trade one ten for ten ones",
		"72 = 7 tens + 2 ones = 6 tens + 12 ones",
		"The carried 1 in 46+27 means one ten",
		"rename across the zero",
		"Addition can be checked with subtraction",
		"subtraction can be checked with addition",
		"place-value chart turned sideways",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("P3-ADD-SUB missing %q", want)
		}
	}
}

func TestP4GuidedPracticeAndMiniTestDoNotRepeatQuestions(t *testing.T) {
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
		"P4-TIME",
	}

	for _, code := range codes {
		pack := buildTopicLessonPack(model.CurriculumTopic{TopicCode: code})
		guided := pack["guidedPractice"].([]gin.H)
		mini := pack["miniTest"].([]gin.H)
		seen := make(map[string]struct{}, len(guided))
		for _, q := range guided {
			prompt, _ := q["prompt"].(string)
			seen[strings.TrimSpace(prompt)] = struct{}{}
		}
		for _, q := range mini {
			prompt, _ := q["prompt"].(string)
			if _, duplicate := seen[strings.TrimSpace(prompt)]; duplicate {
				t.Fatalf("%s repeats practice question in mini test: %q", code, prompt)
			}
		}
	}
}

func TestP4ParentNotesAreTopicSpecific(t *testing.T) {
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
		"P4-TIME",
	}
	seen := map[string]string{}
	for _, code := range codes {
		pack := buildTopicLessonPack(model.CurriculumTopic{TopicCode: code})
		tabs := pack["tabs"].(gin.H)
		notes := tabs["parentNotes"].([]string)
		topLevelNotes := pack["parentCoachNotes"].([]string)
		if strings.Join(notes, "|") != strings.Join(topLevelNotes, "|") {
			t.Fatalf("%s has mismatched top-level and tab parent notes", code)
		}
		if len(notes) < 2 {
			t.Fatalf("%s has %d parent notes, want at least 2", code, len(notes))
		}
		key := strings.Join(notes, "|")
		if previous := seen[key]; previous != "" {
			t.Fatalf("%s repeats parent notes from %s", code, previous)
		}
		seen[key] = code
	}
}

func TestP4CurriculumSeedMatchesMyPalsThirdEdition(t *testing.T) {
	topics := map[string]seedTopic{}
	for _, s := range curriculumSeedTopics() {
		topics[s.Code] = s
	}
	if _, ok := topics["P4-TIME"]; !ok {
		t.Fatalf("P4-TIME missing from curriculum seed")
	}

	h := &CurriculumHandler{}
	if got := h.guessTopicCode(nil, "Time: 24-Hour Clock"); got != "P4-TIME" {
		t.Fatalf("24-hour clock mapped to %s, want P4-TIME", got)
	}

	maps := curriculumSeedTextbookMappings()
	want := map[string]string{
		"4A|Chapter 1 Lesson 4: Rounding and Estimation":               "P4-WHOLE-100000",
		"4A|Chapter 3 Lesson 3: Dividing by a 1-Digit Number":          "P4-FOUR-OPERATIONS",
		"4B|Chapter 8 Practice 5: Fraction of a Set":                   "P4-FRACTIONS-MIXED",
		"4B|Chapter 13 Practice 2: Composite Figures":                  "P4-AREA-PERIMETER",
		"4B|Chapter 15 Practice 2: 24-Hour Clock":                      "P4-TIME",
		"4B|Chapter 14 Practice 2: Line Graphs":                        "P4-STATISTICS",
		"4A|Chapter 7 Lesson 2: Making Symmetric Figures and Patterns": "P4-GEOMETRY-SYMMETRY",
		"4A|Chapter 5 Lesson 3: Turns and 8-Point Compass":             "P4-GEOMETRY-ANGLES",
	}
	seen := map[string]string{}
	for _, m := range maps {
		seen[m.Book+"|"+m.ChapterTitle] = m.TopicCode
	}
	for key, code := range want {
		if got := seen[key]; got != code {
			t.Fatalf("%s mapped to %q, want %q", key, got, code)
		}
	}
}

func TestP4FourOperationsKeepsAlgorithmMeaning(t *testing.T) {
	pack := buildTopicLessonPack(model.CurriculumTopic{TopicCode: "P4-FOUR-OPERATIONS"})
	raw, err := json.Marshal(pack)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{
		"partial products",
		"quotient digit",
		"algorithm gate",
		"model the story, estimate, calculate by place, then inverse check",
		"Every written algorithm line must pass a meaning audit",
		"Line meaning audit",
		"326 groups of 20",
		"division loop",
		"divide, multiply, subtract, bring down",
		"remainder must be smaller than the divisor",
		"326×20=6520",
		"918 ÷ 3",
		"inverse",
		"part-whole, comparison, or equal groups",
		"Model before algorithm",
		"24 equal groups of 326",
		"Maya bar, Leo bar, and a 376 difference",
		"Words like more, left, altogether, or each can mislead",
		"Concrete → pictorial → abstract",
		"area model box",
		"zero in the quotient",
		"824 ÷ 4",
		"divisor × quotient + remainder",
		"5×126+1=631",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("P4-FOUR-OPERATIONS missing %q", want)
		}
	}
}

func TestP4WholeNumbersIncludesApproximationAndPatternReasoning(t *testing.T) {
	pack := buildTopicLessonPack(model.CurriculumTopic{TopicCode: "P4-WHOLE-100000"})
	raw, err := json.Marshal(pack)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{
		"approximately equal",
		"79,486 ≈ 80,000",
		"99,950 to the nearest hundred is 100,000",
		"38,742 + 1,985 ≈ 39,000 + 2,000 = 41,000",
		"72,560, 68,560, 64,560",
		"Use ≈ for rounded or estimated values; use = only for exact values.",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("P4-WHOLE-100000 missing %q", want)
		}
	}
}

func TestP4NetsDistinguishesNetAnd2DRepresentation(t *testing.T) {
	pack := buildTopicLessonPack(model.CurriculumTopic{TopicCode: "P4-GEOMETRY-NETS"})
	raw, err := json.Marshal(pack)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{
		"A net is a flat face map before folding",
		"A three-question gate comes first: What view is this? Does the face kit match? Can it fold or draw correctly?",
		"Ask the three-question gate: view, face kit, fold/draw check.",
		"A view decision comes first: flat net, folded solid, or finished 2D drawing",
		"A shared edge in a net acts like a hinge",
		"fold neighbours around its shared edges like hinges",
		"Track face slots: front, back, top, bottom, left, right.",
		"Track which face becomes front, back, top, bottom, left, or right after folding",
		"Use a slot ledger: write which face lands in each slot.",
		"Every folded face needs one face slot: front, back, left, right, top or bottom.",
		"A slot ledger names where each face lands after folding.",
		"If two faces try to land in the same slot, the net fails even if the face count is correct.",
		"If needed slots stay empty, the net also fails.",
		"Two faces that share an edge in the net fold around that edge like a hinge",
		"Six squares in one straight row has the right face count, but it folds like a belt around the middle.",
		"Six squares in one straight row are...",
		"not a valid cube net because it makes a belt, not a box",
		"Before answering, ask: What view is this?",
		"A cube net has six squares, but two squares fold into the same face slot. What is true?",
		"A valid net needs both the face kit and the fold test",
		"face kit passes, fold test fails",
		"A 2D representation is a finished-solid sketch",
		"the back, bottom or left faces may be hidden by the drawing angle",
		"Face count is not enough",
		"Six correct faces can still be arranged wrongly",
		"Trusting a six-square straight strip",
		"makes a belt, not a closed cube",
		"slant-edge drawing of a cuboid",
		"A representation shows the finished solid on paper",
		"slanting lines show depth on paper",
		"front rectangle, copy a back rectangle diagonally, then connect matching corners",
		"Hidden back edges can be dashed",
		"A triangular prism has two matching triangular ends",
		"A square pyramid net has one square base and four triangles attached around it",
		"opposite faces must end up across from each other after folding",
		"The slot ledger rule is one face per slot, and every needed slot filled.",
		"A slot collision means two different flat squares try to become the same folded face.",
		"Check for slot collisions: no two faces may write the same slot name.",
		"If the slot ledger repeats a slot, say: face kit passes, slot ledger fails.",
		"Slot collision",
		"top used twice",
		"back missing",
		"The face kit passes, but the slot ledger fails",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("P4-GEOMETRY-NETS missing %q", want)
		}
	}
}

func TestP4AreaPerimeterSeparatesInsideAndOutside(t *testing.T) {
	pack := buildTopicLessonPack(model.CurriculumTopic{TopicCode: "P4-AREA-PERIMETER"})
	raw, err := json.Marshal(pack)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{
		"covering inside or walking outside",
		"The measure decision comes first",
		"cover inside means area; walk outside means perimeter",
		"Composite perimeter is not a formula hunt; it is an outside trace",
		"Ask: Is the story covering inside or walking outside?",
		"tile, paint, cover, floor or surface",
		"Internal split lines help us calculate area",
		"internal split line",
		"perimeter only walks the outside edge",
		"Missing-side work comes before perimeter addition.",
		"Horizontal outside lengths must balance to the full width",
		"For a missing horizontal outside side, use full horizontal length minus known horizontal parts.",
		"Use an outside-walk ledger: start corner, walk clockwise, record each outside side, return to start.",
		"Make a perimeter ledger: list each outside side once, in walking order.",
		"Write an outside-walk ledger in order",
		"the split line can help area but is crossed out for perimeter",
		"Skipping the outside-walk ledger",
		"Without a walking list, children often miss a side or count an internal split line.",
		"Before tracing perimeter, rebuild missing outside lengths.",
		"top + right + bottom + step + inner vertical outside + left",
		"Decide cover inside or walk outside before calculating",
		"side 36÷4=9 cm",
		"7×7=49",
		"full top length is 12 cm",
		"An L-shape is split into 7×4 and 3×5 rectangles",
		"Divide by 4 works for square perimeter, not square area",
		"Same area can have different perimeter",
		"same perimeter can enclose different areas",
		"A 6×4 rectangle and an 8×3 rectangle both have area 24 cm²",
		"Which outside-walk ledger is safest?",
		"start corner, walk outside, list each side once, return to start",
		"total height is 10 cm and two vertical parts are 6 cm and 3 cm",
		"Equal area does not mean equal perimeter",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("P4-AREA-PERIMETER missing %q", want)
		}
	}
}

func TestP4FactorsChooseBetweenEqualPacksAndCycles(t *testing.T) {
	pack := buildTopicLessonPack(model.CurriculumTopic{TopicCode: "P4-FACTORS-MULTIPLES"})
	raw, err := json.Marshal(pack)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{
		"exact sharing or repeated cycles meeting",
		"factors answer 'Can one total split exactly?'",
		"multiples answer 'When do repeated jumps land together?'",
		"Story words guide the model",
		"exactly/no leftover points to factors",
		"together/again points to multiples",
		"both numbers often means common",
		"Underline the story job words",
		"No leftover, exactly, equal packs and equal pieces point to factors",
		"Factor stories start with one fixed total",
		"Ask the deciding question first",
		"largest equal pack size with no leftovers",
		"first common multiple is useful when repeated cycles meet again",
		"Equal packs with no leftovers need common factors",
		"A bus leaves every 12 min and a train leaves every 15 min",
		"greatest common factor of 18 and 24",
		"An array model shows factor pairs",
		"T-chart keeps factor pairs organised",
		"Common factors answer equal-pack questions",
		"common multiples answer same-time or next-together questions",
		"Common means the answer must pass both numbers",
		"For common factors, circle only candidates that divide both totals exactly.",
		"For common multiples, circle only landing points reached by both jumps.",
		"Common factor filter",
		"Least common multiple filter",
		"first landing point shared by both",
		"Rows and columns make the factor pair visible",
		"Ask whether the story is about one total being split or repeated jumps meeting",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("P4-FACTORS-MULTIPLES missing %q", want)
		}
	}
}

func TestP4FractionsConnectRenameWholeAndSetModels(t *testing.T) {
	pack := buildTopicLessonPack(model.CurriculumTopic{TopicCode: "P4-FRACTIONS-MIXED"})
	raw, err := json.Marshal(pack)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{
		"Fractions are numbers, but the first skill is choosing the right model.",
		"rename without changing the amount",
		"Use a model gate before calculating: whole, equal parts, job, model.",
		"Use the model gate: whole, equal parts, job, model.",
		"Name the job: rename, place on number line, compare, find part of a set, or combine/subtract same-sized pieces.",
		"Model gate",
		"Which model gate is complete?",
		"whole, equal parts, job, model",
		"Which model fits 3/5 of 25?",
		"The word of plus a count points to a set model.",
		"Using one model for every fraction",
		"Choose the model from the job",
		"Model choice",
		"7/4 asks where a fraction number sits",
		"The first question is: what is the whole?",
		"The whole matters",
		"Use the same whole before comparing fractions",
		"Choose the model: bar/number line for a fraction as a number, set model for fraction of a set",
		"For fraction of a set, find one unit fraction first, then multiply",
		"On a number line, 9/4",
		"30 split into 5 groups, take 2 groups",
		"unsafe without more information",
		"unsafe to compare without knowing the wholes",
		"same-sized fractional parts",
		"Equal parts must be same-sized",
		"3/4 - 1/6",
		"9/12 - 2/12 = 7/12",
		"Maya read 3/4 of a book",
		"1/2 = 2/4",
		"benchmark fractions",
		"Half of eighths is 4/8",
		"18 ÷ 3 = 6, then 2 groups make 12",
		"Twelfths can rename both fourths and sixths",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("P4-FRACTIONS-MIXED missing %q", want)
		}
	}
}

func TestP4DecimalsIncludeMoneyPlaceValue(t *testing.T) {
	pack := buildTopicLessonPack(model.CurriculumTopic{TopicCode: "P4-DECIMALS"})
	raw, err := json.Marshal(pack)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{
		"Money uses two decimal places",
		"Money can be checked in cents",
		"Use the decimal gate before calculating",
		"unit, place, estimate, format",
		"place-value fence",
		"digits left and right of the fence have different jobs",
		"Do not erase the fence when comparing or writing money",
		"For money answers, write the dollar sign and exactly two decimal places",
		"A cents answer is an intermediate check",
		"The final answer gate checks unit, symbol, and decimal places before the answer is finished.",
		"If you use cents, convert back: 313 cents = $3.13, not $313",
		"Run the final answer gate: does the answer have the requested unit, symbol, and format?",
		"Cents are not final format",
		"1000-687=313 cents, which is $3.13",
		"1000 - 687 = 313 cents. What is the final money answer?",
		"After calculating money, ask: Does it have a dollar sign? Does it have two decimal places?",
		"Stopping at the cents check",
		"The cents check gives 313c. Which final answer passes the final answer gate?",
		"The final answer gate checks symbol and two decimal places. 313c becomes $3.13.",
		"change = amount paid - cost",
		"$10.00 - $6.87 = $3.13",
		"The decimal point separates dollars and cents",
		"$12.50 - $4.75 = ?",
		"Decimal × whole number means repeated equal decimal groups",
		"Decimal ÷ whole number means sharing a decimal amount equally",
		"2.4 × 3 means 2.4 + 2.4 + 2.4 = 7.2",
		"4.8 ÷ 4 means share 4.8 into 4 equal parts",
		"3.6 × 4 = ?",
		"9.6 ÷ 4 = ?",
		"1.2×4 must rebuild 4.8",
		"0.305 is 305 thousandths",
		"Trailing zeros",
		"Tenths are bigger than hundredths",
		"$9 and 100 cents",
		"313 cents must be converted back to $3.13",
		"$313 means three hundred thirteen dollars",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("P4-DECIMALS missing %q", want)
		}
	}
}

func TestP4TimeContrastsNoonAndMidnightBoundaries(t *testing.T) {
	pack := buildTopicLessonPack(model.CurriculumTopic{TopicCode: "P4-TIME"})
	raw, err := json.Marshal(pack)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{
		"start + duration = finish",
		"finish - duration = start",
		"Use the time gate before calculating",
		"missing part, direction, unit, boundary",
		"Finish missing means jump forward",
		"Start missing means jump backward",
		"Duration missing means measure the gap",
		"Which time gate is correct for a missing finish time?",
		"start + duration, jump forward",
		"If start time is missing, what should the timeline do?",
		"jump backward from the finish",
		"2 min 45 s = 165 s",
		"00:15 is 12:15 am",
		"16:10 - 45 min = 15:25",
		"same duration can cross a different boundary",
		"11:00 am + 1 h 30 min = 12:30 pm",
		"11:00 pm + 1 h 30 min = 12:30 am",
		"23:30 + 1 h 10 min = ?",
		"00:40",
		"After 23:59 comes 00:00",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("P4-TIME missing %q", want)
		}
	}
}

func TestP4AnglesIncludeNotationAndVertexReasoning(t *testing.T) {
	pack := buildTopicLessonPack(model.CurriculumTopic{TopicCode: "P4-GEOMETRY-ANGLES"})
	raw, err := json.Marshal(pack)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{
		"job decision",
		"am I naming, measuring, drawing, turning, or using shape properties?",
		"Every angle question has a job: name, measure, draw, turn, or use properties.",
		"A good answer names the evidence: vertex, base 0° scale, compass steps, or shape property.",
		"Ask: what job is this question asking me to do?",
		"Point to the evidence before answering.",
		"Before calculating, name the job: angle name, angle measure, draw an angle, compass turn, or shape property.",
		"Name uses the vertex. Measure or draw uses the protractor 0° scale.",
		"angle notation",
		"In ∠ABC, B is the vertex",
		"The middle letter names the corner",
		"Which angle name has vertex Q?",
		"A protractor has two scales",
		"an acute angle cannot read 130°",
		"A turn ledger prevents compass mistakes: start direction, path direction, landing points, step count, then degrees.",
		"If it is compass turning, write a turn ledger: start, direction, landing points, step count, degrees.",
		"Turn ledger",
		"NE→E→SE→S. There are 3 jumps, not 4 labels",
		"Counting compass labels instead of jumps",
		"Which turn ledger is correct for NE to S clockwise?",
		"NE→E→SE→S, 3 jumps, 135°",
		"S to NE anticlockwise",
		"draw an 8 cm by 5 cm rectangle",
		"A shape is a rectangle only if it has four right angles",
		"A square must pass both tests: four right angles and four equal sides.",
		"opposite sides parallel and equal",
		"four 4 cm sides and check every corner is a right angle",
		"A rough drawing is not enough",
		"Four 4 cm sides and four 90° corners",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("P4-GEOMETRY-ANGLES missing %q", want)
		}
	}
}

func TestP4SymmetryIncludesAnchorPointCompletion(t *testing.T) {
	pack := buildTopicLessonPack(model.CurriculumTopic{TopicCode: "P4-GEOMETRY-SYMMETRY"})
	raw, err := json.Marshal(pack)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{
		"copying anchor points",
		"Corners and endpoints",
		"same distance on the opposite side",
		"Anchor points create the skeleton",
		"Label original anchor points A, B, C before copying A', B', C'.",
		"Copy corners, endpoints and dots first.",
		"connect them in the same order",
		"inner details must reflect too",
		"A point on the mirror line reflects to itself",
		"horizontal mirror line",
		"same shape, same size, same distance, opposite side",
		"same area is not enough",
		"two-pass check: outline first, then inner details",
		"Outline match is only the first pass",
		"Do a two-pass check: outline first, then dots, holes, colours and inner marks.",
		"Inner detail check",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("P4-GEOMETRY-SYMMETRY missing %q", want)
		}
	}
}

func TestP4StatisticsStaysWithTablesAndLineGraphs(t *testing.T) {
	pack := buildTopicLessonPack(model.CurriculumTopic{TopicCode: "P4-STATISTICS"})
	raw, err := json.Marshal(pack)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{
		"Statistics is evidence reading",
		"My Pals P4 focuses on tables and line graphs",
		"Use the statistics gate: job, display, scale, evidence.",
		"Use the statistics gate: what job, which display, what scale, what evidence sentence?",
		"Evidence means the actual value plus the unit",
		"A steep line is not evidence until the exact values and scale have been checked.",
		"Statistics gate",
		"Write an evidence sentence: The data shows ___ because ___.",
		"Complete a table",
		"Line graphs need ordered data",
		"Do not connect unrelated categories",
		"first decide what question the data should answer",
		"Table first when the question asks for an exact row",
		"Line graph only when the horizontal labels have a meaningful order",
		"Name the question type: exact value, compare, total, trend, or graph choice",
		"Question job first: exact value, compare, total, trend, or graph choice.",
		"Before touching numbers, ask whether the question wants one exact value, a comparison, a total, a trend, or the best graph type.",
		"Total job",
		"A graph-choice question asks which display protects the meaning",
		"Exact value, compare, total, trend, and graph choice are different jobs",
		"Favourite fruits should not be connected by a line",
		"Wednesday was 28°C",
		"Exact values and scale tell the real change",
		"Which statistics gate is strongest?",
		"job, display, scale, evidence",
		"Making a claim without evidence",
		"Which sentence gives evidence?",
		"Sales increased from 92 to 96, by 4 items",
		"Scale audit comes before trend claims.",
		"Scale audit before trend",
		"A steep line is a clue, not proof.",
		"direction + amount + unit + interval",
		"Sales increased by 4 items from Mon to Thu",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("P4-STATISTICS missing %q", want)
		}
	}
	for _, notWant := range []string{"Pie chart", "pie chart"} {
		if strings.Contains(text, notWant) {
			t.Fatalf("P4-STATISTICS should not include %q in P4 pack: %s", notWant, text)
		}
	}
}

func TestTextbookLessonCardsAreVisibleAndLessonSpecific(t *testing.T) {
	topic := model.CurriculumTopic{
		TopicCode: "P4-TIME",
		Title:     "Seconds and 24-Hour Clock",
		Mappings: []model.TextbookMapping{
			{Book: "4B", ChapterTitle: "Chapter 15: Time", Source: "parent-screenshot", Confidence: "manual"},
			{Book: "4B", ChapterTitle: "Chapter 15 Practice 1: Seconds", Source: "my-pals-3rd-edition-contents", Confidence: "confirmed"},
			{Book: "4B", ChapterTitle: "Chapter 15 Practice 2: 24-Hour Clock", Source: "my-pals-3rd-edition-contents", Confidence: "confirmed"},
			{Book: "4B", ChapterTitle: "Chapter 15 Practice 2: 24-Hour Clock", Source: "parent-screenshot", Confidence: "manual"},
		},
	}

	cards := buildTextbookLessonCards(topic)
	if len(cards) != 2 {
		t.Fatalf("cards len=%d, want 2 concrete lesson cards after filtering duplicates", len(cards))
	}
	if cards[0]["chapterTitle"] != "Chapter 15 Practice 1: Seconds" {
		t.Fatalf("first card=%v, want Seconds practice", cards[0]["chapterTitle"])
	}
	if cards[1]["chapterTitle"] != "Chapter 15 Practice 2: 24-Hour Clock" {
		t.Fatalf("second card=%v, want 24-Hour Clock practice", cards[1]["chapterTitle"])
	}

	examples := cards[1]["workedExamples"].([]gin.H)
	body, _ := examples[0]["body"].(string)
	if body == "Use a timeline and jump to the next easy time first." {
		t.Fatalf("24-hour lesson card still uses generic time example")
	}
	if body == "" {
		t.Fatalf("24-hour lesson card has empty first example")
	}
}

func TestP4AngleLessonCardsAreLessonSpecific(t *testing.T) {
	topic := model.CurriculumTopic{
		TopicCode: "P4-GEOMETRY-ANGLES",
		Title:     "Angles, Rectangles and Squares",
		Mappings: []model.TextbookMapping{
			{Book: "4A", ChapterTitle: "Chapter 5 Lesson 3: Turns and 8-Point Compass", Source: "my-pals-3rd-edition-contents", Confidence: "confirmed"},
			{Book: "4A", ChapterTitle: "Chapter 6 Lesson 1: Properties of Squares and Rectangles", Source: "my-pals-3rd-edition-contents", Confidence: "confirmed"},
		},
	}

	cards := buildTextbookLessonCards(topic)
	if len(cards) != 2 {
		t.Fatalf("cards len=%d, want 2", len(cards))
	}
	for _, card := range cards {
		if card["visualKind"] != "angle" {
			t.Fatalf("%s visualKind=%v, want angle", card["chapterTitle"], card["visualKind"])
		}
	}
	if got := lessonObjective("Chapter 5 Lesson 1: Understanding and Measuring Angles", topic.Title); !strings.Contains(got, "Understand and measure angles") {
		t.Fatalf("angle lesson objective=%q, want natural wording", got)
	}
	compassRaw, _ := json.Marshal(cards[0])
	compass := string(compassRaw)
	for _, want := range []string{"45°", "N to E", "8-point compass"} {
		if !strings.Contains(compass, want) {
			t.Fatalf("compass lesson card missing %q: %s", want, compass)
		}
	}
	propsRaw, _ := json.Marshal(cards[1])
	props := string(propsRaw)
	for _, want := range []string{"Every rectangle", "4 right angles", "special rectangle"} {
		if !strings.Contains(props, want) {
			t.Fatalf("properties lesson card missing %q: %s", want, props)
		}
	}
}

func TestP4FourOperationsLessonCardsUseLessonQuestions(t *testing.T) {
	topic := model.CurriculumTopic{
		TopicCode: "P4-FOUR-OPERATIONS",
		Title:     "Four Operations Algorithms",
		Mappings: []model.TextbookMapping{
			{Book: "4A", ChapterTitle: "Chapter 3 Lesson 1: Multiplying by a 1-Digit Number", Source: "my-pals-3rd-edition-contents", Confidence: "confirmed"},
			{Book: "4A", ChapterTitle: "Chapter 3 Lesson 2: Multiplying by a 2-Digit Number", Source: "my-pals-3rd-edition-contents", Confidence: "confirmed"},
			{Book: "4A", ChapterTitle: "Chapter 3 Lesson 3: Dividing by a 1-Digit Number", Source: "my-pals-3rd-edition-contents", Confidence: "confirmed"},
		},
	}

	cards := buildTextbookLessonCards(topic)
	if len(cards) != 3 {
		t.Fatalf("cards len=%d, want 3", len(cards))
	}
	wantPrompt := []string{"426 × 3 means", "326 × 24 should be split", "936 ÷ 3 starts"}
	for i, card := range cards {
		if card["visualKind"] != "algorithm" {
			t.Fatalf("%s visualKind=%v, want algorithm", card["chapterTitle"], card["visualKind"])
		}
		qs := card["practiceQuestions"].([]gin.H)
		prompt := qs[0]["prompt"].(string)
		if !strings.Contains(prompt, wantPrompt[i]) {
			t.Fatalf("%s first prompt=%q, want to contain %q", card["chapterTitle"], prompt, wantPrompt[i])
		}
	}
}

func TestP4FractionLessonCardsUseFractionExamples(t *testing.T) {
	topic := model.CurriculumTopic{
		TopicCode: "P4-FRACTIONS-MIXED",
		Title:     "Mixed Numbers, Improper Fractions and Fraction of a Set",
		Mappings: []model.TextbookMapping{
			{Book: "4B", ChapterTitle: "Chapter 8 Practice 4: Comparing and Ordering Fractions", Source: "my-pals-3rd-edition-contents", Confidence: "confirmed"},
			{Book: "4B", ChapterTitle: "Chapter 9 Practice 1: Adding Fractions", Source: "my-pals-3rd-edition-contents", Confidence: "confirmed"},
		},
	}

	cards := buildTextbookLessonCards(topic)
	if len(cards) != 2 {
		t.Fatalf("cards len=%d, want 2", len(cards))
	}
	for _, card := range cards {
		if card["visualKind"] != "fractionBars" {
			t.Fatalf("%s visualKind=%v, want fractionBars", card["chapterTitle"], card["visualKind"])
		}
		raw, _ := json.Marshal(card)
		text := string(raw)
		if strings.Contains(text, "58,402") {
			t.Fatalf("%s still uses whole-number comparing example: %s", card["chapterTitle"], text)
		}
		if !strings.Contains(text, "fraction") && !strings.Contains(text, "Fractions") {
			t.Fatalf("%s does not look fraction-specific: %s", card["chapterTitle"], text)
		}
	}
}

func TestP4TextbookLessonCardsDoNotUseGenericFallback(t *testing.T) {
	topics := map[string]model.CurriculumTopic{}
	for _, s := range curriculumSeedTopics() {
		if s.Grade != "P4" {
			continue
		}
		topics[s.Code] = model.CurriculumTopic{TopicCode: s.Code, Title: s.Title}
	}
	for _, m := range curriculumSeedTextbookMappings() {
		if !strings.HasPrefix(m.TopicCode, "P4-") {
			continue
		}
		topic := topics[m.TopicCode]
		topic.Mappings = append(topic.Mappings, model.TextbookMapping{
			Book:         m.Book,
			ChapterTitle: m.ChapterTitle,
			Source:       m.Source,
			Confidence:   m.Confidence,
		})
		topics[m.TopicCode] = topic
	}

	bad := []string{
		"Which model helps most for this lesson?",
		"Name the model before calculating.",
	}
	for code, topic := range topics {
		for _, card := range buildTextbookLessonCards(topic) {
			raw, _ := json.Marshal(card)
			text := string(raw)
			for _, phrase := range bad {
				if strings.Contains(text, phrase) {
					t.Fatalf("%s %s still uses generic fallback %q: %s", code, card["chapterTitle"], phrase, text)
				}
			}
		}
	}
}

func TestP4DecimalAndAreaLessonCardsAreSpecific(t *testing.T) {
	decimalTopic := model.CurriculumTopic{
		TopicCode: "P4-DECIMALS",
		Title:     "Decimals up to 3 Decimal Places",
		Mappings: []model.TextbookMapping{
			{Book: "4B", ChapterTitle: "Chapter 10 Practice 4: Comparing and Ordering Decimals", Source: "my-pals-3rd-edition-contents", Confidence: "confirmed"},
			{Book: "4B", ChapterTitle: "Chapter 10 Practice 5: Rounding Decimals", Source: "my-pals-3rd-edition-contents", Confidence: "confirmed"},
			{Book: "4B", ChapterTitle: "Chapter 11 Practice 2: Subtraction", Source: "my-pals-3rd-edition-contents", Confidence: "confirmed"},
			{Book: "4B", ChapterTitle: "Chapter 12 Practice 1: Solving Decimal Word Problems (1) (p.99)", Source: "parent-screenshot", Confidence: "manual"},
		},
	}
	decimalCards := buildTextbookLessonCards(decimalTopic)
	for _, card := range decimalCards {
		raw, _ := json.Marshal(card)
		text := string(raw)
		if strings.Contains(text, "58,402") || strings.Contains(text, "62,705") {
			t.Fatalf("%s still uses whole-number content: %s", card["chapterTitle"], text)
		}
		if !strings.Contains(text, "decimal") && !strings.Contains(text, "$3.13") {
			t.Fatalf("%s does not look decimal-specific: %s", card["chapterTitle"], text)
		}
	}

	areaTopic := model.CurriculumTopic{
		TopicCode: "P4-AREA-PERIMETER",
		Title:     "Area and Perimeter of Composite Figures",
		Mappings: []model.TextbookMapping{
			{Book: "4B", ChapterTitle: "Chapter 13 Practice 1: Perimeter of Rectangles and Squares (p.119)", Source: "parent-screenshot", Confidence: "manual"},
			{Book: "4B", ChapterTitle: "Chapter 13 Practice 2: Area of Rectangles and Squares (p.121)", Source: "parent-screenshot", Confidence: "manual"},
			{Book: "4B", ChapterTitle: "Chapter 13 Practice 2: Composite Figures", Source: "my-pals-3rd-edition-contents", Confidence: "confirmed"},
		},
	}
	areaCards := buildTextbookLessonCards(areaTopic)
	for _, card := range areaCards {
		raw, _ := json.Marshal(card)
		text := string(raw)
		if !strings.Contains(text, "area") && !strings.Contains(text, "perimeter") && !strings.Contains(text, "Composite") {
			t.Fatalf("%s does not look area/perimeter-specific: %s", card["chapterTitle"], text)
		}
	}
}

func TestP4SymmetryMakingLessonIsNotCapturedByRecognitionCase(t *testing.T) {
	topic := model.CurriculumTopic{
		TopicCode: "P4-GEOMETRY-SYMMETRY",
		Title:     "Line Symmetry",
		Mappings: []model.TextbookMapping{
			{Book: "4A", ChapterTitle: "Chapter 7 Lesson 2: Making Symmetric Figures and Patterns", Source: "my-pals-3rd-edition-contents", Confidence: "confirmed"},
		},
	}
	cards := buildTextbookLessonCards(topic)
	raw, _ := json.Marshal(cards[0])
	text := string(raw)
	if !strings.Contains(text, "Complete the shape") || !strings.Contains(text, "copy each point") {
		t.Fatalf("making symmetry lesson did not use construction-specific content: %s", text)
	}
}

func requireP4Tabs(t *testing.T, code string, pack map[string]any) {
	t.Helper()
	tabs, ok := pack["tabs"].(gin.H)
	if !ok {
		t.Fatalf("%s missing tabs", code)
	}
	for _, key := range []string{"bigIdea", "visualModel", "workedExamples", "practice", "miniTest", "challenge", "parentNotes", "review"} {
		if _, ok := tabs[key]; !ok {
			t.Fatalf("%s missing tab %s", code, key)
		}
	}
	model, ok := tabs["visualModel"].(gin.H)
	if !ok {
		t.Fatalf("%s visualModel tab has unexpected type %T", code, tabs["visualModel"])
	}
	if model["visualKind"] == "" || model["visualKind"] == "generic" {
		t.Fatalf("%s has weak visual kind %q", code, model["visualKind"])
	}
	practice := tabs["practice"].([]gin.H)
	mini := tabs["miniTest"].([]gin.H)
	if practice[0]["prompt"] == mini[0]["prompt"] {
		t.Fatalf("%s tab practice and mini test start with the same question", code)
	}
	review, ok := tabs["review"].(gin.H)
	if !ok {
		t.Fatalf("%s review tab has unexpected type %T", code, tabs["review"])
	}
	if checks, ok := review["misconceptionChecks"].([]gin.H); !ok || len(checks) < 3 {
		t.Fatalf("%s review misconceptionChecks too shallow: %#v", code, review["misconceptionChecks"])
	}
	if rubric, ok := review["masteryRubric"].([]gin.H); !ok || len(rubric) != 4 {
		t.Fatalf("%s review masteryRubric must cover four domains: %#v", code, review["masteryRubric"])
	}
	if prompts, ok := review["reviewPrompts"].([]gin.H); !ok || len(prompts) < 3 {
		t.Fatalf("%s reviewPrompts too shallow: %#v", code, review["reviewPrompts"])
	}
}

func requireNoDebugChoiceText(t *testing.T, code string, pack map[string]any) {
	t.Helper()
	for _, key := range []string{"guidedPractice", "miniTest", "challenge"} {
		for _, q := range pack[key].([]gin.H) {
			for _, opt := range q["options"].([]string) {
				if strings.Contains(opt, "? no") {
					t.Fatalf("%s %s option contains debug text: %q", code, key, opt)
				}
			}
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

func TestP4FactorsMultiplesStoryProblemsNameTheirPicture(t *testing.T) {
	pack := buildTopicLessonPack(model.CurriculumTopic{TopicCode: "P4-FACTORS-MULTIPLES"})
	const bags = "factor-story-bags.svg"
	const calendar = "multiple-story-calendar.svg"

	withDiagram := func(items []gin.H) (all []gin.H, factor, multiple int) {
		for _, it := range items {
			d, _ := it["diagram"].(string)
			if d == "" {
				continue
			}
			if d != bags && d != calendar {
				t.Fatalf("unexpected diagram %q", d)
			}
			if _, err := os.Stat(filepath.Join("..", "..", "web", "static", "curriculum", d)); err != nil {
				t.Fatalf("diagram %s missing on disk: %v", d, err)
			}
			base := strings.TrimSuffix(d, ".svg")
			if _, err := os.Stat(filepath.Join("..", "..", "web", "static", "curriculum", base+".excalidraw")); err != nil {
				t.Fatalf("diagram source %s.excalidraw missing: %v", base, err)
			}
			all = append(all, it)
			if d == bags {
				factor++
			} else {
				multiple++
			}
		}
		return
	}

	examples, f, m := withDiagram(pack["workedExamples"].([]gin.H))
	if f != 2 || m != 2 {
		t.Fatalf("story worked examples: %d factor, %d multiple, want 2 each", f, m)
	}
	for _, ex := range examples {
		body, _ := ex["body"].(string)
		for _, step := range []string{"Which story is it", "Draw it", "List and circle", "Size check", "Say the answer back"} {
			if !strings.Contains(body, step) {
				t.Fatalf("story example %v lacks step %q", ex["title"], step)
			}
		}
		for _, want := range []string{"share equally", "meet again"} {
			if !strings.Contains(body, want) {
				t.Fatalf("story example %v lacks %q", ex["title"], want)
			}
		}
		if !strings.Contains(body, "never bigger than the smaller total") || !strings.Contains(body, "never smaller than the bigger number") {
			t.Fatalf("story example %v lacks the size check rule", ex["title"])
		}
	}

	guided, f, m := withDiagram(pack["guidedPractice"].([]gin.H))
	if len(guided) != 6 || f != 4 || m != 2 {
		t.Fatalf("guided story questions=%d factor=%d multiple=%d, want 6 (4 bags, 2 calendar)", len(guided), f, m)
	}
	pairs := 0
	for i, q := range guided {
		prompt, _ := q["prompt"].(string)
		if !strings.HasPrefix(prompt, "Which picture fits this story: equal bags or jumps that meet?") {
			continue
		}
		if i+1 >= len(guided) || guided[i+1]["diagram"] != q["diagram"] {
			t.Fatalf("picture question %q is not followed by its numeric question", prompt)
		}
		want := "Equal bags"
		if q["diagram"] == calendar {
			want = "Jumps that meet"
		}
		if q["correct"] != want {
			t.Fatalf("picture question %q correct=%v, want %q", prompt, q["correct"], want)
		}
		pairs++
	}
	if pairs != 3 {
		t.Fatalf("picture/numeric pairs=%d, want 3", pairs)
	}

	mini, _, _ := withDiagram(pack["miniTest"].([]gin.H))
	if len(mini) != 5 {
		t.Fatalf("mini story questions=%d, want 5", len(mini))
	}
	guidedPrompts := map[any]bool{}
	for _, q := range guided {
		guidedPrompts[q["prompt"]] = true
	}
	var rangeQ, trapQ gin.H
	for _, q := range mini {
		if guidedPrompts[q["prompt"]] {
			t.Fatalf("mini question repeats guided practice: %v", q["prompt"])
		}
		prompt, _ := q["prompt"].(string)
		if strings.Contains(prompt, "multiple of 3 and of 4") && strings.Contains(prompt, "between 30 and 50") {
			rangeQ = q
		}
		if strings.Contains(prompt, "shared equally among 4 children or among 6 children") {
			trapQ = q
		}
	}
	if rangeQ == nil || rangeQ["correct"] != "36 and 48" {
		t.Fatalf("range problem missing or wrong: %v", rangeQ)
	}
	if trapQ == nil || trapQ["correct"] != "12" || trapQ["diagram"] != calendar {
		t.Fatalf("keyword trap missing or wrong: %v", trapQ)
	}

	found := false
	for _, mis := range pack["commonMisconceptions"].([]gin.H) {
		if mis["mistake"] == "Choosing factor or multiple from one keyword" {
			found = true
			fix, _ := mis["fix"].(string)
			for _, want := range []string{"share equally", "common multiple", "size check"} {
				if !strings.Contains(strings.ToLower(fix), want) {
					t.Fatalf("misconception fix lacks %q: %s", want, fix)
				}
			}
		}
	}
	if !found {
		t.Fatal("misconception about choosing from one keyword is missing")
	}

	notes, _ := json.Marshal(pack["parentCoachNotes"])
	if !strings.Contains(string(notes), "先让她画出袋子或标记") {
		t.Fatalf("parent coach notes lack the drawing line: %s", notes)
	}
}
