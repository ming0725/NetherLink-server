package knowledge

type Query struct {
	Query        string
	ViewerUserID string
	ScopeType    string
	ScopeID      string
	Limit        int
}

type Result struct {
	DocumentID string
	ChunkID    string
	SourceType string
	SourceID   string
	Title      *string
	Content    string
	Score      float64
}
