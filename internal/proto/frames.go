package proto

// Frame type constants. The wire `type` field carries these strings.
const (
	TypeHello           = "hello"
	TypeOpenSession     = "open_session"
	TypeCloseSession    = "close_session"
	TypeSessionStarted  = "session.started"
	TypeSessionExited   = "session.exited"
	TypePTYResize       = "pty.resize"
	TypePTYControlEvent = "pty.control_event"
	TypeJSONLTail       = "jsonl.tail"
	TypePing            = "ping"
	TypePong            = "pong"
	TypeCatalogDiff     = "catalog.diff"
	TypeSearchRequest   = "search.request"
	TypeSearchResults   = "search.results"
)

// Wrapper -> server.

type Hello struct {
	WrapperID    string   `json:"wrapper_id"`
	OS           string   `json:"os"`
	Arch         string   `json:"arch"`
	Version      string   `json:"version"`
	Accounts     []string `json:"accounts"`
	Capabilities []string `json:"capabilities"`
	// Sessions is the list of sessions currently alive on this wrapper,
	// sent on every hello (reconnect-aware). Empty on first connect.
	Sessions []HelloSession `json:"sessions"`
}

type HelloSession struct {
	ID                   string `json:"id"`
	PID                  int    `json:"pid"`
	JSONLUUID            string `json:"jsonl_uuid"`
	Cwd                  string `json:"cwd"`
	Account              string `json:"account"`
	BytesSinceDisconnect int    `json:"bytes_since_disconnect"`
}

type SessionStarted struct {
	PID       int    `json:"pid"`
	JSONLUUID string `json:"jsonl_uuid"`
	Cwd       string `json:"cwd"`
	Account   string `json:"account"`
}

type SessionExited struct {
	ExitCode int    `json:"exit_code"`
	Reason   string `json:"reason"` // "normal" | "signal" | "wrapper_close" | "spawn_failed"
	Detail   string `json:"detail,omitempty"`
}

type PTYControlEvent struct {
	Event  string `json:"event"` // "resize_ack" | "error"
	Detail string `json:"detail,omitempty"`
}

type JSONLTail struct {
	Entry string `json:"entry"`
}

type Pong struct {
	Echo string `json:"echo"`
}

// Server -> wrapper.

type OpenSession struct {
	Session string   `json:"session"`
	Cwd     string   `json:"cwd"`
	Account string   `json:"account"`
	Args    []string `json:"args,omitempty"`
}

type CloseSession struct{}

type PTYResize struct {
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}

type Ping struct {
	Nonce string `json:"nonce"`
}

// CatalogDiff is sent by the wrapper to keep the server-side catalog in
// sync with ~/.claude/projects/. On every reconnect the first frame must
// have Full=true: the server treats it as ground truth for that wrapper
// and deletes anything not present. Subsequent frames during a connection
// carry only the delta (Full=false).
type CatalogDiff struct {
	Full               bool                 `json:"full"`
	Projects           []CatalogProject     `json:"projects"`
	Transcripts        []CatalogTranscript  `json:"transcripts"`
	RemovedTranscripts []string             `json:"removed_transcripts"`
}

type CatalogProject struct {
	Slug            string `json:"slug"`
	Cwd             string `json:"cwd"`
	Name            string `json:"name"`
	SessionCount    int    `json:"session_count"`
	FirstActivityAt string `json:"first_activity_at"`
	LastActivityAt  string `json:"last_activity_at"`
}

type CatalogTranscript struct {
	JSONLUUID    string `json:"jsonl_uuid"`
	Slug         string `json:"slug"`
	Path         string `json:"path"`
	StartedAt    string `json:"started_at"`
	EndedAt      string `json:"ended_at"`
	MessageCount int    `json:"message_count"`
	Title        string `json:"title"`
	Bytes        int64  `json:"bytes"`
}

// SearchRequest is sent by the server when a portal user starts a search.
// The envelope's `session` field carries the request id used to correlate
// the response.
type SearchRequest struct {
	Query           string   `json:"query"`
	ProjectID       string   `json:"project_id,omitempty"`
	TranscriptIDs   []string `json:"transcript_ids,omitempty"`
	MaxResults      int      `json:"max_results"`
	SnippetChars    int      `json:"snippet_chars"`
	CaseInsensitive bool     `json:"case_insensitive"`
}

// SearchResults is the wrapper's reply to a SearchRequest. Empty Matches
// is valid (no hits). Truncated=true means MaxResults or the per-search
// timeout was hit.
type SearchResults struct {
	Matches   []SearchMatch `json:"matches"`
	Truncated bool          `json:"truncated"`
	ElapsedMs int64         `json:"elapsed_ms"`
}

type SearchMatch struct {
	TranscriptID string `json:"transcript_id"`
	MsgIndex     int    `json:"msg_index"`
	Role         string `json:"role"`
	Snippet      string `json:"snippet"`
	Timestamp    string `json:"ts,omitempty"`
}
