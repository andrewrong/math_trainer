package model

import (
	"encoding/json"
	"time"
)

// ─── Math ────────────────────────────────────────────────────────────────────

type MathSession struct {
	ID        int64     `json:"id"`
	CreatedAt time.Time `json:"date"`
	Area      string    `json:"area"`
	Level     string    `json:"level"`
	TypeKey   string    `json:"typeKey"`
	Total     int       `json:"total"`
	Correct   int       `json:"correct"`
	Rate      int       `json:"rate"`
	Elapsed   int       `json:"elapsed"`
}

type SaveSessionReq struct {
	Area    string `json:"area"    binding:"required"`
	Level   string `json:"level"   binding:"required"`
	TypeKey string `json:"typeKey" binding:"required"`
	Total   int    `json:"total"`
	Correct int    `json:"correct"`
	Rate    int    `json:"rate"`
	Elapsed int    `json:"elapsed"`
}

type WrongAnswer struct {
	Key      string       `json:"key"`
	Category string       `json:"category"`
	Q        QuestionData `json:"q"`
	Count    int          `json:"count"`
}

type QuestionData struct {
	A   int    `json:"a"`
	Op  string `json:"op"`
	B   int    `json:"b"`
	Ans int    `json:"ans"`
	Key string `json:"key"`
}

// Delta: +1 to increment, -1 to decrement (auto-delete at 0)
type UpsertWrongReq struct {
	Key      string `json:"key"      binding:"required"`
	Category string `json:"category" binding:"required"`
	A        int    `json:"a"`
	Op       string `json:"op"`
	B        int    `json:"b"`
	Ans      int    `json:"ans"`
	Delta    int    `json:"delta"`
}

type MasteredFact struct {
	FactKey string `json:"factKey"`
	Status  string `json:"status"` // "ok" | "weak"
}

type SetMasteredReq struct {
	Status string `json:"status" binding:"required,oneof=ok weak"`
}

// ─── English ─────────────────────────────────────────────────────────────────

type WordEntry struct {
	Word       string   `json:"word"`
	Syllables  string   `json:"syllables"`
	Pos        string   `json:"pos"`
	PhonicsTip string   `json:"phonicsTip"`
	Definition string   `json:"definition"`
	Example    string   `json:"example"`
	Examples   []string `json:"examples,omitempty"`
}

type BlankEntry struct {
	Word     string `json:"word"`
	Sentence string `json:"sentence"`
}

type SpellScore struct {
	Correct int `json:"correct"`
	Wrong   int `json:"wrong"`
}

type WeekData struct {
	WeekID      string                `json:"weekId"`
	Label       string                `json:"label"`
	CreatedAt   time.Time             `json:"createdAt"`
	Words       []WordEntry           `json:"words"`
	Story       string                `json:"story"`
	Blanks      []BlankEntry          `json:"blanks"`
	SpellScores map[string]SpellScore `json:"spellScores"`
}

type WeekSummary struct {
	WeekID      string                `json:"weekId"`
	Label       string                `json:"label"`
	CreatedAt   time.Time             `json:"createdAt"`
	WordCount   int                   `json:"wordCount"`
	Words       []string              `json:"words"`
	SpellScores map[string]SpellScore `json:"spellScores"`
}

type GenerateTextReq struct {
	Words []string `json:"words" binding:"required,min=1,max=20"`
}

type UpdateSpellReq struct {
	Word    string `json:"word"    binding:"required"`
	Correct int    `json:"correct"`
	Wrong   int    `json:"wrong"`
}

type SentenceCheckReq struct {
	Word     string `json:"word"     binding:"required"`
	Sentence string `json:"sentence" binding:"required"`
}

type SentenceCheckResp struct {
	OK        bool   `json:"ok"`
	Feedback  string `json:"feedback"`
	Corrected string `json:"corrected,omitempty"`
}

type EnglishDiagnostic struct {
	ID         int64           `json:"id"`
	CreatedAt  time.Time       `json:"createdAt"`
	GradeLevel string          `json:"gradeLevel"`
	Report     json.RawMessage `json:"report"`
}

type SaveEnglishDiagnosticReq struct {
	GradeLevel string          `json:"gradeLevel"`
	Report     json.RawMessage `json:"report" binding:"required"`
}

// ─── Chinese ─────────────────────────────────────────────────────────────────

type ChineseItem struct {
	ID               int64    `json:"id,omitempty"`
	Character        string   `json:"character" binding:"required"`
	Pinyin           string   `json:"pinyin"`
	Tone             string   `json:"tone"`
	Meaning          string   `json:"meaning"`
	Words            []string `json:"words"`
	ExampleSentence  string   `json:"exampleSentence"`
	PinyinHint       string   `json:"pinyinHint"`
	ShapeHint        string   `json:"shapeHint"`
	WrongCount       int      `json:"wrongCount,omitempty"`
	ReviewCount      int      `json:"reviewCount,omitempty"`
	PinyinWrong      int      `json:"pinyinWrong,omitempty"`
	RecognitionWrong int      `json:"recognitionWrong,omitempty"`
	MeaningWrong     int      `json:"meaningWrong,omitempty"`
	WordWrong        int      `json:"wordWrong,omitempty"`
	SentenceWrong    int      `json:"sentenceWrong,omitempty"`
	WeakKind         string   `json:"weakKind,omitempty"`
	LastReviewedAt   string   `json:"lastReviewedAt,omitempty"`
}

type ChineseWeek struct {
	WeekID    string        `json:"weekId"`
	Label     string        `json:"label"`
	CreatedAt time.Time     `json:"createdAt"`
	Items     []ChineseItem `json:"items"`
}

type CreateChineseWeekReq struct {
	Label string        `json:"label" binding:"required"`
	Items []ChineseItem `json:"items" binding:"required,min=1,max=200"`
}

type UpdateChineseReviewReq struct {
	Character string `json:"character" binding:"required"`
	Kind      string `json:"kind" binding:"required"`
	Correct   bool   `json:"correct"`
}

type SaveChineseReviewRoundReq struct {
	StartPos int `json:"startPos"`
	EndPos   int `json:"endPos"`
	Total    int `json:"total"`
	Correct  int `json:"correct"`
	Wrong    int `json:"wrong"`
}

// ─── Piano ───────────────────────────────────────────────────────────────────

type PianoAssignment struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	Section   string `json:"section"`
	Goal      string `json:"goal"`
	DueDate   string `json:"dueDate"`
	Status    string `json:"status"`
	CreatedAt string `json:"createdAt,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

type SavePianoAssignmentReq struct {
	Title   string `json:"title" binding:"required"`
	Section string `json:"section"`
	Goal    string `json:"goal"`
	DueDate string `json:"dueDate"`
	Status  string `json:"status"`
}

type PianoPracticeSession struct {
	ID          int64  `json:"id"`
	PracticedAt string `json:"practicedAt"`
	Minutes     int    `json:"minutes"`
	Content     string `json:"content"`
	Stuck       string `json:"stuck"`
	Note        string `json:"note"`
}

type SavePianoPracticeReq struct {
	Minutes int    `json:"minutes"`
	Content string `json:"content"`
	Stuck   string `json:"stuck"`
	Note    string `json:"note"`
}

type UpdatePianoSkillReq struct {
	Skill   string `json:"skill" binding:"required"`
	Correct bool   `json:"correct"`
}

// ─── Curriculum ──────────────────────────────────────────────────────────────

type CurriculumTopic struct {
	TopicCode     string              `json:"topicCode"`
	Grade         string              `json:"grade"`
	Strand        string              `json:"strand"`
	SubStrand     string              `json:"subStrand"`
	Title         string              `json:"title"`
	Objectives    json.RawMessage     `json:"objectives"`
	Prerequisites json.RawMessage     `json:"prerequisites"`
	Related       json.RawMessage     `json:"related"`
	SortOrder     int                 `json:"sortOrder"`
	Progress      *CurriculumProgress `json:"progress,omitempty"`
	Mappings      []TextbookMapping   `json:"mappings,omitempty"`
}

type CurriculumProgress struct {
	TopicCode     string `json:"topicCode"`
	Status        string `json:"status"`
	ConceptScore  int    `json:"conceptScore"`
	AccuracyScore int    `json:"accuracyScore"`
	SpeedScore    int    `json:"speedScore"`
	TransferScore int    `json:"transferScore"`
	UpdatedAt     string `json:"updatedAt"`
}

type UpdateCurriculumProgressReq struct {
	Status        string `json:"status" binding:"required"`
	ConceptScore  int    `json:"conceptScore"`
	AccuracyScore int    `json:"accuracyScore"`
	SpeedScore    int    `json:"speedScore"`
	TransferScore int    `json:"transferScore"`
}

type TextbookMapping struct {
	ID             int64  `json:"id"`
	TopicCode      string `json:"topicCode"`
	TextbookSeries string `json:"textbookSeries"`
	Book           string `json:"book"`
	ChapterTitle   string `json:"chapterTitle"`
	Source         string `json:"source"`
	Confidence     string `json:"confidence"`
}

type CurriculumArtifact struct {
	ID           int64           `json:"id"`
	TopicCode    string          `json:"topicCode"`
	ArtifactType string          `json:"artifactType"`
	Content      json.RawMessage `json:"content"`
}

type ImportTextbookMapReq struct {
	Book     string `json:"book" binding:"required"`
	Chapters []struct {
		ChapterTitle string `json:"chapterTitle" binding:"required"`
		TopicCode    string `json:"topicCode"`
		Confidence   string `json:"confidence"`
	} `json:"chapters" binding:"required"`
}

// ─── Generic responses ───────────────────────────────────────────────────────

type APIError struct {
	Error string `json:"error"`
	Code  string `json:"code,omitempty"`
}

type OKResponse struct {
	OK bool `json:"ok"`
}
