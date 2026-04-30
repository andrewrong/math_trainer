package model

import "time"

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
	Word       string `json:"word"`
	Syllables  string `json:"syllables"`
	Pos        string `json:"pos"`
	PhonicsTip string `json:"phonicsTip"`
	Definition string `json:"definition"`
	Example    string `json:"example"`
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

// ─── Generic responses ───────────────────────────────────────────────────────

type APIError struct {
	Error string `json:"error"`
	Code  string `json:"code,omitempty"`
}

type OKResponse struct {
	OK bool `json:"ok"`
}
