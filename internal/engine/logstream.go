package engine

import (
	"context"
	"fmt"
	"io"
)

// LogStreamOpts parameterises a follow stream. Tail maps to `compose logs
// --tail <n>` (empty ⇒ the compose default), Since to `--since <dur>` (empty ⇒
// no time bound). Both are strings so "all" / durations pass straight through.
type LogStreamOpts struct {
	Tail  string
	Since string
}

// LogStream starts following a stack's logs and returns a reader over the
// combined stdout+stderr stream plus a cancel func. It reuses this engine's
// runtime and config but redirects compose output into an in-memory pipe, so a
// skin (the TUI) can tail lines without owning any orchestration: the actual
// `<runtime> compose logs -f` still runs behind Logs. The caller reads until
// EOF and calls cancel() to stop following. opts maps to compose's
// --tail/--since flags so tail sizing and time-window jumps stay engine-side.
func (e *Engine) LogStream(ctx context.Context, name, service string, opts LogStreamOpts) (io.ReadCloser, context.CancelFunc, error) {
	streamCtx, cancel := context.WithCancel(ctx)
	pr, pw := io.Pipe()
	sub := &Engine{RT: e.RT, Cfg: e.Cfg, Out: pw, Err: pw}
	go func() {
		err := sub.Logs(streamCtx, name, service, true, opts.Tail, opts.Since)
		// A cancel surfaces as a context error; don't report that as a failure.
		if err != nil && streamCtx.Err() == nil {
			fmt.Fprintf(pw, "\nlog stream ended: %v\n", err)
		}
		pw.Close()
	}()
	return pr, cancel, nil
}
