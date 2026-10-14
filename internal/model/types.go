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
	WeekID         string                `json:"weekId"`
	Label          string                `json:"label"`
	CreatedAt      time.Time             `json:"createdAt"`
	Words          []WordEntry           `json:"words"`
	Story          string                `json:"story"`
	Blanks         []BlankEntry          `json:"blanks"`
	SpellScores    map[string]SpellScore `json:"spellScores"`
	FridayMistakes []string              `json:"fridayMistakes"`
}

type WeekSummary struct {
	WeekID         string                `json:"weekId"`
	Label          string                `json:"label"`
	CreatedAt      time.Time             `json:"createdAt"`
	WordCount      int                   `json:"wordCount"`
	Words          []string              `json:"words"`
	SpellScores    map[string]SpellScore `json:"spellScores"`
	FridayMistakes []string              `json:"fridayMistakes"`
}

type GenerateTextReq struct {
	Words []string `json:"words" binding:"required,min=1,max=20"`
}

type UpdateSpellReq struct {
	Word    string `json:"word"    binding:"required"`
	Correct int    `json:"correct"`
	Wrong   int    `json:"wrong"`
}

// UpdateFridaySpellingReq is parent-entered evidence from a paper spelling test.
// It deliberately records only the words which still need further practice.
type UpdateFridaySpellingReq struct {
	Mistakes []string `json:"mistakes" binding:"max=20"`
}

// RenameEnglishWeekReq changes only the learner-facing label. WeekID remains
// the stable key used by practice history and saved stories.
type RenameEnglishWeekReq struct {
	Label string `json:"label" binding:"required,max=80"`
}

// UpdateEnglishWeekWordsReq replaces a saved vocabulary list while keeping its
// stable week ID and learner-facing name.
type UpdateEnglishWeekWordsReq struct {
	Words []string `json:"words" binding:"required,min=1,max=20"`
}

// EnglishPronunciation keeps British IPA separate from the audio renderer.
// Generated records are deliberately distinct from a parent-confirmed entry.
type EnglishPronunciation struct {
	Word     string `json:"word"`
	Locale   string `json:"locale"`
	IPA      string `json:"ipa"`
	Source   string `json:"source"`
	Verified bool   `json:"verified"`
}

type LookupEnglishPronunciationsReq struct {
	Words []string `json:"words" binding:"required,min=1,max=100"`
}

type SaveEnglishPronunciationReq struct {
	IPA string `json:"ipa" binding:"required,max=80"`
}

type EnglishSpeechReq struct {
	Text string `json:"text" binding:"required,max=200"`
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

// ─── English Curriculum ─────────────────────────────────────────────────────

type EnglishCurriculumUnit struct {
	UnitID    string                     `json:"unitId"`
	Grade     string                     `json:"grade"`
	Book      string                     `json:"book"`
	UnitNo    int                        `json:"unitNo"`
	Title     string                     `json:"title"`
	PageStart int                        `json:"pageStart"`
	SortOrder int                        `json:"sortOrder"`
	Progress  *EnglishCurriculumProgress `json:"progress,omitempty"`
}

type EnglishCurriculumProgress struct {
	UnitID          string `json:"unitId"`
	Status          string `json:"status"`
	ReadingScore    int    `json:"readingScore"`
	VocabularyScore int    `json:"vocabularyScore"`
	LanguageScore   int    `json:"languageScore"`
	WritingScore    int    `json:"writingScore"`
	UpdatedAt       string `json:"updatedAt"`
}

type UpdateEnglishCurriculumProgressReq struct {
	Status          string `json:"status" binding:"required"`
	ReadingScore    int    `json:"readingScore"`
	VocabularyScore int    `json:"vocabularyScore"`
	LanguageScore   int    `json:"languageScore"`
	WritingScore    int    `json:"writingScore"`
}

type EnglishCurriculumImportReq struct {
	Grade string `json:"grade" binding:"required"`
	Book  string `json:"book"  binding:"required"`
	Lines string `json:"lines" binding:"required"`
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
	ToneWrong        int      `json:"toneWrong,omitempty"`
	WritingWrong     int      `json:"writingWrong,omitempty"`
	WeakKind         string   `json:"weakKind,omitempty"`
	LastReviewedAt   string   `json:"lastReviewedAt,omitempty"`
}

type ChineseWeek struct {
	WeekID    string        `json:"weekId"`
	Label     string        `json:"label"`
	CreatedAt time.Time     `json:"createdAt"`
	Items     []ChineseItem `json:"items"`
}

// ChineseContextStory is one deliberately short, checked context exercise for
// a weekly Chinese set. The learner sees a continuous story with each target
// item blanked once; supporting unfamiliar words can carry pinyin annotations.
type ChineseContextStory struct {
	SetKey    string                   `json:"setKey"`
	Sentences []ChineseContextSentence `json:"sentences"`
	Blanks    []ChineseContextBlank    `json:"blanks"`
	CreatedAt time.Time                `json:"createdAt"`
}

type ChineseContextSentence struct {
	Text        string                    `json:"text"`
	Annotations []ChinesePinyinAnnotation `json:"annotations"`
}

type ChinesePinyinAnnotation struct {
	Word   string `json:"word"`
	Pinyin string `json:"pinyin"`
}

type ChineseContextBlank struct {
	Character     string `json:"character"`
	SentenceIndex int    `json:"sentenceIndex"`
	Sentence      string `json:"sentence"`
}

type CreateChineseWeekReq struct {
	Label string        `json:"label" binding:"required"`
	Items []ChineseItem `json:"items" binding:"required,min=1,max=200"`
}

type UpdateChineseReviewReq struct {
	Character string       `json:"character" binding:"required"`
	Kind      string       `json:"kind" binding:"required"`
	Correct   bool         `json:"correct"`
	Item      *ChineseItem `json:"item"`
}

type SaveChineseReviewRoundReq struct {
	StartPos int `json:"startPos"`
	EndPos   int `json:"endPos"`
	Total    int `json:"total"`
	Correct  int `json:"correct"`
	Wrong    int `json:"wrong"`
}

// ─── Piano homework companion ────────────────────────────────────────────────

type PianoWeek struct {
	ID          int64           `json:"id"`
	Label       string          `json:"label"`
	LessonDate  string          `json:"lessonDate"`
	TeacherNote string          `json:"teacherNote"`
	CreatedAt   string          `json:"createdAt"`
	Tasks       []PianoTask     `json:"tasks,omitempty"`
	Assets      []PianoAsset    `json:"assets,omitempty"`
	Feedback    []PianoFeedback `json:"feedback,omitempty"`
}
type PianoTask struct {
	ID         int64  `json:"id"`
	WeekID     int64  `json:"weekId"`
	Kind       string `json:"kind"`
	Title      string `json:"title"`
	RangeLabel string `json:"rangeLabel"`
	Goal       string `json:"goal"`
	Priority   int    `json:"priority"`
	Status     string `json:"status"`
}
type PianoAsset struct {
	ID           int64  `json:"id"`
	WeekID       int64  `json:"weekId"`
	TaskID       int64  `json:"taskId,omitempty"`
	Kind         string `json:"kind"`
	OriginalName string `json:"originalName"`
	MimeType     string `json:"mimeType"`
	OCRText      string `json:"ocrText,omitempty"`
	CreatedAt    string `json:"createdAt"`
}
type PianoFeedback struct {
	ID        int64  `json:"id"`
	WeekID    int64  `json:"weekId"`
	TaskID    int64  `json:"taskId,omitempty"`
	AssetID   int64  `json:"assetId,omitempty"`
	Note      string `json:"note"`
	CreatedAt string `json:"createdAt"`
}
type CreatePianoWeekReq struct {
	Label       string `json:"label" binding:"required,max=100"`
	LessonDate  string `json:"lessonDate"`
	TeacherNote string `json:"teacherNote"`
}
type SavePianoTaskReq struct {
	Kind       string `json:"kind" binding:"required,oneof=technique piece sight-reading theory"`
	Title      string `json:"title" binding:"required,max=160"`
	RangeLabel string `json:"rangeLabel"`
	Goal       string `json:"goal"`
	Priority   int    `json:"priority"`
	Status     string `json:"status"`
}
type SavePianoSessionReq struct {
	TaskID         int64  `json:"taskId"`
	Minutes        int    `json:"minutes"`
	SelfStatus     string `json:"selfStatus" binding:"required,oneof=smooth practising stuck"`
	DifficultyNote string `json:"difficultyNote"`
}
type SavePianoFeedbackReq struct {
	TaskID  int64  `json:"taskId"`
	AssetID int64  `json:"assetId"`
	Note    string `json:"note"`
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
