package model

import "time"

type ReadingProfile struct {
	ProfileID   string `json:"profileId"`
	DisplayName string `json:"displayName"`
	ProfileType string `json:"profileType"`
	ReviewMode  string `json:"reviewMode"`
	AvatarColor string `json:"avatarColor"`
}

type ReadingProgress struct {
	ProfileID string `json:"profileId"`
	BookID    int64  `json:"bookId"`
	Chapter   string `json:"chapter"`
	Page      int    `json:"page"`
	Percent   int    `json:"percent"`
	Status    string `json:"status"`
	UpdatedAt string `json:"updatedAt"`
}

type ReadingBook struct {
	ID             int64            `json:"id"`
	Title          string           `json:"title"`
	Author         string           `json:"author"`
	Genre          string           `json:"genre"`
	Language       string           `json:"language"`
	ISBN           string           `json:"isbn"`
	Publisher      string           `json:"publisher"`
	CoverPath      string           `json:"-"`
	CoverAssetID   int64            `json:"coverAssetId,omitempty"`
	MetadataSource string           `json:"metadataSource"`
	AssetStatus    string           `json:"assetStatus,omitempty"`
	Progress       *ReadingProgress `json:"progress,omitempty"`
	CreatedAt      string           `json:"createdAt"`
}

type BookCandidate struct {
	Title      string  `json:"title"`
	Author     string  `json:"author"`
	Genre      string  `json:"genre"`
	Language   string  `json:"language"`
	ISBN       string  `json:"isbn"`
	Publisher  string  `json:"publisher"`
	Confidence float64 `json:"confidence"`
	RawText    string  `json:"rawText"`
}

type CreateReadingBookReq struct {
	Title          string `json:"title" binding:"required"`
	Author         string `json:"author"`
	Genre          string `json:"genre"`
	Language       string `json:"language"`
	ISBN           string `json:"isbn"`
	Publisher      string `json:"publisher"`
	MetadataSource string `json:"metadataSource"`
}

type ReadingAsset struct {
	ID              int64  `json:"id"`
	BookID          int64  `json:"bookId"`
	AssetType       string `json:"assetType"`
	OriginalName    string `json:"originalName"`
	MimeType        string `json:"mimeType"`
	IngestionStatus string `json:"ingestionStatus"`
	IngestionError  string `json:"ingestionError,omitempty"`
	CreatedAt       string `json:"createdAt"`
}

type ReadingSectionSummary struct {
	ID        int64  `json:"id"`
	Ordinal   int    `json:"ordinal"`
	Title     string `json:"title"`
	PageStart int    `json:"pageStart"`
	PageEnd   int    `json:"pageEnd"`
}

type ReadingSession struct {
	ID           int64       `json:"id"`
	ProfileID    string      `json:"profileId"`
	BookID       int64       `json:"bookId"`
	Book         ReadingBook `json:"book"`
	SessionDate  string      `json:"sessionDate"`
	Chapter      string      `json:"chapter"`
	PageFrom     int         `json:"pageFrom"`
	PageTo       int         `json:"pageTo"`
	Percent      int         `json:"percent"`
	Minutes      int         `json:"minutes"`
	Reaction     string      `json:"reaction"`
	QuickNote    string      `json:"quickNote"`
	ResponseMode string      `json:"responseMode"`
	CreatedAt    time.Time   `json:"createdAt"`
}

type CreateReadingSessionReq struct {
	ProfileID    string `json:"profileId" binding:"required"`
	BookID       int64  `json:"bookId" binding:"required"`
	SessionDate  string `json:"sessionDate"`
	Chapter      string `json:"chapter"`
	PageFrom     int    `json:"pageFrom"`
	PageTo       int    `json:"pageTo"`
	Percent      int    `json:"percent"`
	Minutes      int    `json:"minutes"`
	Reaction     string `json:"reaction"`
	QuickNote    string `json:"quickNote"`
	ResponseMode string `json:"responseMode"`
	Finished     bool   `json:"finished"`
}

type ReadingQuestion struct {
	Skill         string   `json:"skill"`
	Question      string   `json:"question"`
	Difficulty    int      `json:"difficulty"`
	ExpectedIdeas []string `json:"expectedIdeas,omitempty"`
	Evidence      []string `json:"evidence,omitempty"`
	Hint          string   `json:"hint,omitempty"`
}

type ReadingFeedback struct {
	Skill          string `json:"skill"`
	Affirmation    string `json:"affirmation"`
	Hint           string `json:"hint"`
	SuggestedState string `json:"suggestedState"`
}

type ReadingCheck struct {
	ID              int64             `json:"id"`
	ProfileID       string            `json:"profileId"`
	BookID          int64             `json:"bookId"`
	SessionID       int64             `json:"sessionId,omitempty"`
	Tier            string            `json:"tier"`
	SkillFocus      string            `json:"skillFocus"`
	Grounded        bool              `json:"grounded"`
	SourceLabel     string            `json:"sourceLabel"`
	Questions       []ReadingQuestion `json:"questions"`
	Responses       []string          `json:"responses,omitempty"`
	Feedback        []ReadingFeedback `json:"feedback,omitempty"`
	ProviderName    string            `json:"providerName,omitempty"`
	PromptVersion   string            `json:"promptVersion"`
	ContentRevision string            `json:"contentRevision,omitempty"`
	CreatedAt       string            `json:"createdAt"`
}

type CreateReadingCheckReq struct {
	ProfileID string `json:"profileId" binding:"required"`
	BookID    int64  `json:"bookId" binding:"required"`
	SessionID int64  `json:"sessionId"`
	Tier      string `json:"tier" binding:"required"`
	Chapter   string `json:"chapter"`
	PageFrom  int    `json:"pageFrom"`
	PageTo    int    `json:"pageTo"`
}

type RespondReadingCheckReq struct {
	Responses    []string `json:"responses" binding:"required"`
	ResponseMode string   `json:"responseMode"`
}

type ReadingWeeklyReview struct {
	ProfileID  string `json:"profileId"`
	WeekStart  string `json:"weekStart"`
	Reflection string `json:"reflection"`
	NextGoal   string `json:"nextGoal"`
	Reviewer   string `json:"reviewer"`
}
