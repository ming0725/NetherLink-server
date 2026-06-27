package memory

type Entry struct {
	ID         string
	AgentID    string
	UserID     *string
	ScopeType  string
	ScopeID    *string
	MemoryType string
	Content    string
	Confidence float64
}

type Candidate struct {
	ScopeType            string
	ScopeID              *string
	MemoryType           string
	Content              string
	Confidence           float64
	RequiresConfirmation bool
}
