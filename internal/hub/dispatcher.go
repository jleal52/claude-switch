// Package hub will hold the in-memory routing tables in Task 19. For now,
// only the Dispatcher interface lives here so the api package can depend
// on a stable contract.
package hub

import "context"

type OpenSessionRequest struct {
	WrapperID string
	SessionID string
	Cwd       string
	Account   string
	Args      []string
}

// Dispatcher is implemented by the hub and consumed by /api/sessions.
// Tests pass a fake implementation.
type Dispatcher interface {
	OpenSession(ctx context.Context, req OpenSessionRequest) error
	CloseSession(ctx context.Context, sessionID string) error
}
