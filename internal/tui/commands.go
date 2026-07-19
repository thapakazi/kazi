package tui

import (
	"bufio"
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/thapakazi/kazi/internal/engine"
)

// cmdTimeout bounds each engine read so a hung runtime cannot wedge the UI.
const cmdTimeout = 10 * time.Second

// startLogStreamCmd opens a follow stream for a stack's logs (no timeout — it is
// meant to run until the user leaves the tab). opts carries the active tail size
// and since-window; changing either (t/s) restarts the stream. It returns a
// logStreamMsg with a scanner the model then pumps via readLogCmd.
func startLogStreamCmd(eng Engine, stack, service string, opts engine.LogStreamOpts) tea.Cmd {
	return func() tea.Msg {
		r, cancel, err := eng.LogStream(context.Background(), stack, service, opts)
		if err != nil {
			return errMsg{err}
		}
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		return logStreamMsg{stack: stack, service: service, reader: r, cancel: cancel, scanner: sc}
	}
}

// readLogCmd reads the next line from a stream; it returns a logLineMsg or, at
// EOF/cancel, a logDoneMsg. Each line self-schedules the next read in Update.
func readLogCmd(scanner *bufio.Scanner, stack string) tea.Cmd {
	return func() tea.Msg {
		if scanner.Scan() {
			return logLineMsg{stack: stack, line: scanner.Text()}
		}
		return logDoneMsg{stack: stack}
	}
}

// tickCmd schedules the next refresh tick.
func tickCmd(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// snapshotCmd reads every container (Ps) plus the grouped stack list (List) so
// the sidebar can show registered/discovered/unmanaged/system in one pass. It
// runs off the UI goroutine and returns a snapshotMsg or errMsg.
func snapshotCmd(eng Engine) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
		defer cancel()
		stacks, err := eng.List(ctx)
		if err != nil {
			return errMsg{err}
		}
		all, err := eng.Ps(ctx)
		if err != nil {
			return errMsg{err}
		}
		var loose []engine.ContainerInfo
		for _, c := range all {
			if c.Kind == engine.KindUnmanaged {
				loose = append(loose, c)
			}
		}
		return snapshotMsg{stacks: stacks, loose: loose}
	}
}

// statusbarCmd reads the doctor-lite signals for the status bar.
func statusbarCmd(eng Engine) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
		defer cancel()
		gc := eng.GcDebris(ctx)
		proxyUp := false
		if st, err := eng.Status(ctx, "kazi-proxy"); err == nil && st.Running > 0 {
			proxyUp = true
		}
		return statusbarMsg{runtime: runtimeName(eng), proxyUp: proxyUp, gcCount: gc}
	}
}

// statusCmd reads per-service detail for one stack (Services tab).
func statusCmd(eng Engine, name string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
		defer cancel()
		info, err := eng.Status(ctx, name)
		if err != nil {
			return errMsg{err}
		}
		return statusMsg{stack: name, info: info}
	}
}

// urlsCmd reads endpoints for one stack (URLs tab).
func urlsCmd(eng Engine, name string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
		defer cancel()
		eps, err := eng.Urls(ctx, name)
		if err != nil {
			return errMsg{err}
		}
		return urlsMsg{stack: name, endpoints: eps}
	}
}

// describeCmd reads the effective/merged detail for one stack (Config tab).
func describeCmd(eng Engine, name string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
		defer cancel()
		d, err := eng.Describe(ctx, name)
		if err != nil {
			return errMsg{err}
		}
		return describeMsg{stack: name, detail: d}
	}
}

// templatesCmd reads the catalog list (Catalog mode).
func templatesCmd(eng Engine) tea.Cmd {
	return func() tea.Msg {
		ts, err := eng.TemplateList()
		if err != nil {
			return errMsg{err}
		}
		return templatesMsg{templates: ts}
	}
}

// removeCmd dispatches the guarded x:delete action (deregister a stack). The
// result comes back as an actionDoneMsg the model turns into a toast + refresh.
func removeCmd(eng Engine, name string) tea.Cmd {
	return func() tea.Msg {
		return actionDoneMsg{action: "delete", stack: name, err: eng.Remove(name)}
	}
}

// openUrlCmd resolves a stack's openable HTTP endpoints (o:open). The result is
// an openResolvedMsg the model turns into a direct open, a picker, or a toast.
func openUrlCmd(eng Engine, name string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
		defer cancel()
		eps, err := eng.Urls(ctx, name)
		if err != nil {
			return errMsg{err}
		}
		var choices []urlChoice
		for _, e := range eps {
			if e.Kind == "http" && e.URL != "" {
				label := e.URL
				if e.Service != "" {
					label = e.Service + " → " + e.URL
				}
				choices = append(choices, urlChoice{label: label, url: e.URL})
			}
		}
		return openResolvedMsg{stack: name, choices: choices}
	}
}

// actionCap bounds the captured action-log ring.
const actionCap = 300

// actionStartCmd launches a lifecycle verb (up/down/restart) with its output
// captured, so compose progress fills the Action panel instead of the terminal.
func actionStartCmd(eng Engine, action, name string) tea.Cmd {
	return func() tea.Msg {
		r, errc := eng.ActionStream(context.Background(), action, name)
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		return actionStreamMsg{action: action, stack: name, scanner: sc, errc: errc}
	}
}

// readActionCmd pumps the next captured line, or at EOF waits for the verb's
// final error and reports it as an actionDoneMsg.
func readActionCmd(sc *bufio.Scanner, errc <-chan error, action, stack string) tea.Cmd {
	return func() tea.Msg {
		if sc.Scan() {
			return actionLineMsg{line: sc.Text()}
		}
		return actionDoneMsg{action: action, stack: stack, err: <-errc}
	}
}

// openCmd launches the browser opener for a URL and reports the result.
func openCmd(url string) tea.Cmd {
	return func() tea.Msg {
		return openedMsg{url: url, err: browserOpen(url)}
	}
}

// runtimeName reports the engine's active runtime name for the status bar.
// The read Engine seam doesn't expose it directly, so the concrete engine is
// probed when available; tests' fake engine falls back to "docker".
func runtimeName(eng Engine) string {
	if e, ok := eng.(*engine.Engine); ok && e.RT != nil {
		return e.RT.Name()
	}
	return "docker"
}
