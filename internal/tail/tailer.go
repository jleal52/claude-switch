package tail

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"time"
)

// Tail reads path line-by-line, calling emit for each line. Existing content
// is emitted first, then the tailer polls for appended content until ctx is
// cancelled. Returns nil on ctx cancel, error on open/read failure.
func Tail(ctx context.Context, path string, emit func(string)) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	r := bufio.NewReader(f)
	for {
		line, err := r.ReadString('\n')
		if len(line) > 0 {
			// Strip trailing newline.
			if line[len(line)-1] == '\n' {
				line = line[:len(line)-1]
			}
			emit(line)
		}
		if err == nil {
			continue
		}
		if !errors.Is(err, io.EOF) {
			return err
		}
		// EOF: wait for more data or ctx cancel.
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(50 * time.Millisecond):
		}
	}
}
