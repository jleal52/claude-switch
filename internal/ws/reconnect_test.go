package ws

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/require"

	"github.com/jleal52/claude-switch/internal/session"
)

func TestBackoffIsExponentialWithCap(t *testing.T) {
	b := NewBackoff(100*time.Millisecond, 2*time.Second)
	d0 := b.Next()
	d1 := b.Next()
	d2 := b.Next()
	d10 := time.Duration(0)
	for i := 0; i < 10; i++ {
		d10 = b.Next()
	}

	require.GreaterOrEqual(t, d0, 100*time.Millisecond)
	require.LessOrEqual(t, d0, 150*time.Millisecond) // +jitter up to 50%
	require.Greater(t, d1, d0/2)                      // roughly doubled minus jitter
	require.Greater(t, d2, d1/2)
	require.LessOrEqual(t, d10, 2*time.Second+time.Duration(float64(2*time.Second)*0.5))
	// Reset puts us back to base.
	b.Reset()
	dr := b.Next()
	require.LessOrEqual(t, dr, 150*time.Millisecond)

	_ = math.Pi // silence unused import if any
}

func TestRunOnceExitsWhenNoPingArrives(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, _ := websocket.Accept(w, r, nil)
		defer c.CloseNow()
		_, _, _ = c.Read(r.Context()) // consume hello; then idle until close.
		<-r.Context().Done()
	}))
	defer srv.Close()

	events := make(chan session.Event, 4)
	sup := session.NewSupervisor(session.Config{ClaudeBin: "/bin/true"}, events)
	cli := NewClient(Config{
		URL:         "ws" + srv.URL[len("http"):],
		Token:       "t",
		WrapperID:   "w",
		Version:     "test",
		ReadTimeout: 100 * time.Millisecond,
	}, sup, events)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := cli.runOnce(ctx)
	require.Error(t, err) // a timeout, not a clean exit
}
