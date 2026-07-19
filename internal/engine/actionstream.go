package engine

import (
	"context"
	"fmt"
	"io"
)

// ActionStream runs a lifecycle verb (up/down/restart) with its compose output
// captured into a reader instead of the engine's Out/Err, so a skin (the TUI)
// can show progress in a panel rather than letting compose scribble over an
// alt-screen. The verb's final error is delivered on the returned channel once
// the reader reaches EOF.
func (e *Engine) ActionStream(ctx context.Context, action, name string) (io.ReadCloser, <-chan error) {
	pr, pw := io.Pipe()
	sub := &Engine{RT: e.RT, Cfg: e.Cfg, Out: pw, Err: pw}
	errc := make(chan error, 1)
	go func() {
		var err error
		switch action {
		case "up":
			err = sub.Up(ctx, name)
		case "down":
			err = sub.Down(ctx, name)
		case "restart":
			err = sub.Restart(ctx, name)
		default:
			err = fmt.Errorf("unknown action %q", action)
		}
		pw.Close()
		errc <- err
	}()
	return pr, errc
}
