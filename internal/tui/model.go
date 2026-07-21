package tui

import (
	"bufio"
	"context"
	"io"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/thapakazi/kazi/internal/engine"
	"github.com/thapakazi/kazi/internal/template"
)

// mode is the top-level view (Tab / 1 / 2 toggles).
type mode int

const (
	modeStacks mode = iota
	modeCatalog
)

// focus is which pane owns motion keys.
type focus int

const (
	focusSidebar focus = iota
	focusDetail
)

// detailTab indexes the detail-pane tabs.
type detailTab int

const (
	tabServices detailTab = iota
	tabLogs
	tabEnv
	tabURLs
	tabConfig
	tabStats
)

var tabNames = []string{"Services", "Logs", "Env", "URLs", "Config", "Stats"}

// actionKind identifies a guarded engine action a confirm modal dispatches.
type actionKind int

const (
	actNone actionKind = iota
	actDelete
	actKeep // promote a watched ephemeral stack (k:keep)
	actGc   // reclaim a watched ephemeral stack (g:gc)
)

// modalKind distinguishes a yes/no confirm from a list picker.
type modalKind int

const (
	modalConfirm      modalKind = iota
	modalPicker                 // choose a URL to open
	modalMenu                   // stack quick-actions (s)
	modalOpenChoose             // transient open menu (o → b browser / e editor)
	modalEditOpen               // choose config vs project to open in $EDITOR (o → e)
	modalSourceChoose           // transient new-stack source picker (n → c/t/i)
	modalRemoveChoose           // transient remove/teardown picker (d → d/r)
	modalLogService             // transient Logs container filter picker (c)
	modalEnvService             // transient Env container filter picker (c)
	modalStatsService           // transient Stats container filter picker (c)
)

// modalState is the active modal. active==false means none is open; while one
// is open, polling pauses and keys route to the modal. Confirm modals use
// action/stack; picker modals use options/values/cursor.
type modalState struct {
	active bool
	mkind  modalKind
	prompt string

	// confirm
	stack  string
	action actionKind

	// picker
	options []string // display labels
	values  []string // parallel payload (e.g. URLs)
	cursor  int
}

// rowKind classifies a flattened sidebar row.
type rowKind int

const (
	rowAll    rowKind = iota // synthetic ALL overview
	rowHeader                // group header (REGISTERED/…), not selectable target
	rowStack                 // a real stack or loose container
)

// sidebarRow is one flattened, rendered line in the sidebar. Headers are
// skipped by selection movement; only rowAll and rowStack are selectable.
type sidebarRow struct {
	kind    rowKind
	label   string // group name or stack name
	stack   *engine.StackInfo
	selKind selKind // for contextualKeys when this row is selected
	running int
	total   int
}

// Model is the root bubbletea model for the dashboard.
type Model struct {
	eng  Engine
	keys keyMap
	st   styles

	mode  mode
	focus focus
	tab   detailTab

	// sidebar state
	rows []sidebarRow
	sel  int // index into rows; always points at a selectable row

	// catalog state
	templates []template.Info
	catSel    int

	// detail caches for the selected stack
	statusInfo   engine.StackInfo
	statusName   string
	endpoints    []engine.Endpoint
	endpointsFor string
	detail       engine.StackDetail
	detailFor    string

	// Logs tab: live `compose logs -f` stream for the selected stack. Only one
	// stream runs at a time; navigating away tears it down via logCancel.
	logStack     string
	logService   string
	logLines     []string
	logReader    io.ReadCloser
	logCancel    context.CancelFunc
	logScanner   *bufio.Scanner
	logStreaming bool

	// M5-Log viewer state, all client-side over logLines except tail/since
	// (which restart the stream): follow toggle, tail & since ladders,
	// incremental search + match cursor, pattern grouping, and a scroll offset.
	logFollow    bool
	logTail      string
	logSince     string
	logSearch    string
	logSearching bool
	logMatchCur  int
	logGrouped   bool
	logScroll    int
	// logFullscreen expands the Logs viewport into a near-fullscreen popup
	// (margins on all sides) so long log lines get the full terminal width.
	logFullscreen bool

	// Env tab: each container's `.Config.Env`, cached per stack (env is fixed at
	// container creation). envService is the per-container display filter (empty ⇒
	// all), the Env-tab analogue of the Logs container filter; envScroll offsets
	// the (client-side) viewport.
	env        []engine.ContainerEnv
	envFor     string
	envService string
	envScroll  int
	// Env search mirrors the Logs viewer: incremental /-search over the displayed
	// (filtered) rows with an n/N match cursor.
	envSearch    string
	envSearching bool
	envMatchCur  int

	// Stats tab: live `<runtime> stats` stream for the selected stack. Mirrors the
	// Logs lifecycle — one stream at a time, torn down on leave via statsCancel.
	// statsSeries ring-buffers per-container CPU%/Mem% for the sparklines; the
	// engine stays stateless (it just emits samples). statsErr holds a
	// runtime-unavailable message so the tab degrades without killing the UI.
	statsStack      string
	statsService    string
	statsStreaming  bool
	statsCancel     context.CancelFunc
	statsCh         <-chan engine.StatSample
	statsSeries     map[string]*statSeries
	statsErr        string
	statsHistory    int // sparkline ring size (spec.tui.statsHistory, default 60)
	statsFullscreen bool

	// Host overview (ALL): host CPU/Mem/Disk graphs + an aggregate container-usage
	// line, polled on the normal tick while ALL is selected (hostInFlight guards
	// against overlapping polls, since the aggregate blocks ~1s on a stats delta).
	hostStats    engine.HostStats
	hostHave     bool
	hostInFlight bool
	hostCPUHist  []float64
	hostMemHist  []float64
	aggCPU       float64
	aggMem       uint64
	aggStacks    int

	// Action panel: captured output of the last lifecycle verb (up/down/restart),
	// shown in a collapsible bottom bar so compose progress never scribbles over
	// the dashboard.
	actionTitle   string // e.g. "up dozzle"
	actionLines   []string
	actionRunning bool
	actionOpen    bool // panel expanded vs. collapsed
	actionScroll  int  // lines scrolled up from the latest (0 = pinned to bottom)
	actionScanner *bufio.Scanner
	actionErrc    <-chan error
	actionVerb    string
	actionName    string

	// status bar signals
	runtimeName string
	proxyUp     bool
	gcCount     int

	filter    string
	filtering bool
	help      bool

	// modal is the active guarded-action confirmation (inactive ⇒ no modal).
	// toast is a transient result banner; toastSeq lets a later toast cancel an
	// older one's scheduled clear.
	modal    modalState
	toast    string
	toastSeq int

	// form is the active input form (create n / try t); inactive ⇒ none. Keys
	// route to it while active. pendingSelect is a stack to focus once it lands
	// in the next snapshot (a freshly created/tried stack).
	form          formState
	pendingSelect string

	// watchStack is the ephemeral stack most recently launched via the try form;
	// while it's the selection, k:keep / g:gc are offered on it.
	watchStack string

	width, height int
	refresh       time.Duration
	stale         bool
	err           error
}

// defaultStatsHistory is the Stats-tab sparkline ring size when none is
// configured — ~2min of history at the runtime's ~1s stats cadence.
const defaultStatsHistory = 60

// Option tweaks a freshly built Model. It keeps New's signature stable (existing
// callers and tests pass none) while letting the CLI thread config through.
type Option func(*Model)

// WithStatsHistory sets the Stats-tab sparkline ring size; non-positive values
// keep the default.
func WithStatsHistory(n int) Option {
	return func(m *Model) {
		if n > 0 {
			m.statsHistory = n
		}
	}
}

// New builds a dashboard model over the given engine with the given refresh
// interval. The model starts on the synthetic ALL overview.
func New(eng Engine, refresh time.Duration, opts ...Option) Model {
	if refresh <= 0 {
		refresh = 2 * time.Second
	}
	m := Model{
		eng:          eng,
		keys:         defaultKeyMap(),
		st:           defaultStyles(),
		refresh:      refresh,
		width:        80,
		height:       24,
		logFollow:    true,
		logTail:      "500",
		logSince:     "all",
		statsHistory: defaultStatsHistory,
	}
	for _, o := range opts {
		o(&m)
	}
	return m
}

// Init kicks off the first load and starts the refresh ticker.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		snapshotCmd(m.eng),
		statusbarCmd(m.eng),
		templatesCmd(m.eng),
		loadActionHistoryCmd(),
		tickCmd(m.refresh),
	)
}

// selectedRow returns the currently selected sidebar row, or nil.
func (m Model) selectedRow() *sidebarRow {
	if m.sel < 0 || m.sel >= len(m.rows) {
		return nil
	}
	return &m.rows[m.sel]
}

// currentSelection resolves the contextual selection for the keybar.
func (m Model) currentSelection() selection {
	if m.mode == modeCatalog {
		if len(m.templates) > 0 {
			return selection{kind: selTemplate}
		}
		return selection{kind: selNone}
	}
	r := m.selectedRow()
	if r == nil || r.kind != rowStack {
		return selection{kind: selNone}
	}
	return selection{
		kind:    r.selKind,
		running: r.running > 0,
		watched: m.watchStack != "" && r.label == m.watchStack,
	}
}
