package tui

import (
	"bufio"
	"context"
	"io"
	"time"

	"github.com/thapakazi/kazi/internal/engine"
	"github.com/thapakazi/kazi/internal/template"
)

// tickMsg fires on the refresh ticker; carries the moment it fired.
type tickMsg time.Time

// snapshotMsg is the result of one sidebar read (Ps groups every container,
// including unmanaged loose ones). Stacks is derived from List for grouping.
type snapshotMsg struct {
	stacks []engine.StackInfo
	loose  []engine.ContainerInfo
}

// statusMsg carries per-service detail for the selected stack (Services tab).
type statusMsg struct {
	stack string
	info  engine.StackInfo
}

// urlsMsg carries endpoints for the selected stack (URLs tab).
type urlsMsg struct {
	stack     string
	endpoints []engine.Endpoint
}

// describeMsg carries the effective/merged detail for the selected stack (Config tab).
type describeMsg struct {
	stack  string
	detail engine.StackDetail
}

// envMsg carries each container's environment for the selected stack (Env tab).
type envMsg struct {
	stack string
	env   []engine.ContainerEnv
}

// statusbarMsg carries the always-on doctor-lite signals.
type statusbarMsg struct {
	runtime string
	proxyUp bool
	gcCount int
}

// templatesMsg carries the catalog list (Catalog mode).
type templatesMsg struct {
	templates []template.Info
}

// logStreamMsg reports that a `compose logs -f` stream has started for a stack;
// it hands the model the reader, its cancel func, and a line scanner.
type logStreamMsg struct {
	stack, service string
	reader         io.ReadCloser
	cancel         context.CancelFunc
	scanner        *bufio.Scanner
}

// logLineMsg carries one streamed log line, tagged with its stack so a stale
// stream (the user navigated away) can be discarded.
type logLineMsg struct {
	stack string
	line  string
}

// logDoneMsg marks a stream's end (EOF or cancel).
type logDoneMsg struct{ stack string }

// actionStreamMsg reports that a captured lifecycle verb (up/down/restart) has
// started; it hands the model the scanner + error channel to pump.
type actionStreamMsg struct {
	action, stack string
	scanner       *bufio.Scanner
	errc          <-chan error
}

// actionLineMsg carries one captured line of a lifecycle verb's output.
type actionLineMsg struct{ line string }

// actionHistoryMsg carries the tail of the persisted action log, loaded at
// startup so the panel can show past actions before anything runs this session.
type actionHistoryMsg struct{ lines []string }

// actionDoneMsg reports the result of a guarded action (e.g. x:delete) or a
// finished lifecycle verb; err is nil on success. The model toasts + refreshes.
type actionDoneMsg struct {
	action string
	stack  string
	err    error
}

// toastClearMsg clears the toast if it's still the one that scheduled this
// (matched by seq), so a newer toast isn't wiped early.
type toastClearMsg struct{ seq int }

// urlChoice is one openable HTTP endpoint: a display label and the URL to open.
type urlChoice struct {
	label string
	url   string
}

// openResolvedMsg carries a stack's openable URLs after an o:open key: zero ⇒
// toast, one ⇒ open directly, many ⇒ a picker modal.
type openResolvedMsg struct {
	stack   string
	choices []urlChoice
}

// openedMsg reports the result of launching the browser opener.
type openedMsg struct {
	url string
	err error
}

// errMsg flags a failed engine read; the model keeps its last good frame and
// raises the staleness flag rather than blanking the screen.
type errMsg struct{ err error }

// createDoneMsg reports the create form's result for any source (compose add /
// template try / image run). On error the form stays open with an inline
// message; on success it closes and the new stack is selected on the next
// snapshot.
type createDoneMsg struct {
	name string
	err  error
}

// tryDoneMsg reports the try form's launch result. On success the view focuses
// the new ephemeral stack (watchStack); on error a toast surfaces the failure.
type tryDoneMsg struct {
	name string
	err  error
}

// routeDoneMsg reports how many static routes were added from a stack's
// published ports (s-menu → route).
type routeDoneMsg struct {
	stack string
	count int
}

// tryValuesMsg carries a template's values for the try form (t:try in Catalog).
type tryValuesMsg struct {
	tmpl   string
	values []engine.TryValue
}

// editTargetsMsg carries a stack's resolved edit targets for the o-e open flow:
// they map to config/project choices, one opening the editor directly and two
// opening a picker.
type editTargetsMsg struct {
	stack   string
	targets []engine.EditTarget
	err     error
}

// editorOpenedMsg reports the result of launching the detached external editor.
type editorOpenedMsg struct {
	path string
	err  error
}
