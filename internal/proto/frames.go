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
