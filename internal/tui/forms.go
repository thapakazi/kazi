package tui

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/thapakazi/kazi/internal/engine"
)

// formKind distinguishes the create form (n) from the Catalog try values form (t).
type formKind int

const (
	formNone formKind = iota
	formCreate
	formTry
)

// fieldKind distinguishes a free-text field from a ‹ a · b · c › cycler.
type fieldKind int

const (
	fieldText fieldKind = iota
	fieldChoice
)

// createSources are the source types the create form can register, in cycle order.
var createSources = []string{"compose", "template", "image"}

// tvPrefix keys a create-form field that holds one template value; the suffix is
// the value key. It lets sticky values be tracked per template.
const tvPrefix = "tv:"

// formField is one line in a form: free-text or a choice cycler. For fieldText,
// value is what the user typed; for fieldChoice, value mirrors choices[choiceIdx].
type formField struct {
	key        string
	label      string
	kind       fieldKind
	value      string
	choices    []string
	choiceIdx  int
	mustChange bool
	orig       string // template default — used to compute --set deltas
}

// formState is an active input form floating over the dashboard. While active,
// keys route to it (handleFormKey). The create form is a small source machine:
// srcIdx/tmplSel drive which adaptive fields are shown, vals stickily preserves
// typed text across rebuilds, and tmplCache holds fetched template values.
type formState struct {
	active bool
	kind   formKind
	title  string
	tmpl   string // formTry (Catalog) only
	fields []formField
	cursor int
	err    string

	// create-form machine
	srcIdx    int
	tmplSel   int
	tmplNames []string
	vals      map[string]string
	tmplCache map[string][]engine.TryValue
}

// sourceChoice is one entry in the transient new-stack source chooser.
type sourceChoice struct {
	key  string
	name string
	desc string
}

// sourceChoices are the options shown after n (Emacs transient style): one
// keystroke picks the source and opens that source's form directly.
var sourceChoices = []sourceChoice{
	{"c", "compose", "a compose file or project directory"},
	{"t", "template", "start from a catalog template"},
	{"i", "image", "a container image (e.g. redis:7)"},
}

// openSourceForm opens the new-stack form for a chosen source (compose/template/
// image), showing only that source's fields. Template names come from the
// already-loaded catalog. Returns a fetch command for the template source.
func (m *Model) openSourceForm(src string) tea.Cmd {
	idx := 0
	for i, s := range createSources {
		if s == src {
			idx = i
		}
	}
	names := make([]string, len(m.templates))
	for i, t := range m.templates {
		names[i] = t.Name
	}
	m.form = formState{
		active:    true,
		kind:      formCreate,
		title:     "new " + src + " stack",
		srcIdx:    idx,
		tmplNames: names,
		vals:      map[string]string{},
		tmplCache: map[string][]engine.TryValue{},
	}
	return m.rebuildCreateFields()
}

// rebuildCreateFields regenerates the create form's fields for the chosen source
// (and, for templates, the selected template + its cached values). It returns a
// fetch command when the selected template's values aren't cached yet.
func (m *Model) rebuildCreateFields() tea.Cmd {
	f := &m.form
	if f.srcIdx < 0 || f.srcIdx >= len(createSources) {
		f.srcIdx = 0
	}
	src := createSources[f.srcIdx]

	// url is the *.localhost subdomain; blank ⇒ the stack name. Offered for every
	// source since any routable stack gets a hostname.
	fields := []formField{
		{key: "name", label: "name", kind: fieldText, value: f.vals["name"]},
		{key: "url", label: urlLabel(src), kind: fieldText, value: f.vals["url"]},
	}

	var fetch string
	switch src {
	case "compose":
		fields = append(fields, formField{key: "path", label: "compose file or directory", kind: fieldText, value: f.vals["path"]})
	case "image":
		fields = append(fields,
			formField{key: "image", label: "image (e.g. redis:7)", kind: fieldText, value: f.vals["image"]},
			formField{key: "ports", label: "ports (host:container, …)", kind: fieldText, value: f.vals["ports"]},
			formField{key: "env", label: "env (KEY=value, …)", kind: fieldText, value: f.vals["env"]},
			formField{key: "volumes", label: "volumes (name:/path, …)", kind: fieldText, value: f.vals["volumes"]},
		)
	case "template":
		if len(f.tmplNames) == 0 {
			fields = append(fields, formField{key: "_none", label: "template", kind: fieldText,
				value: "(no templates — add one with kazi template)"})
			break
		}
		if f.tmplSel < 0 || f.tmplSel >= len(f.tmplNames) {
			f.tmplSel = 0
		}
		tmpl := f.tmplNames[f.tmplSel]
		fields = append(fields, formField{key: "template", label: "template", kind: fieldChoice,
			choices: f.tmplNames, choiceIdx: f.tmplSel, value: tmpl})
		if vals, ok := f.tmplCache[tmpl]; ok {
			for _, v := range vals {
				key := tvPrefix + v.Key
				val := v.Value
				if stored, edited := f.vals[key]; edited {
					val = stored
				}
				fields = append(fields, formField{key: key, label: v.Key, kind: fieldText,
					value: val, mustChange: v.MustChange, orig: v.Value})
			}
		} else {
			fetch = tmpl
		}
	}

	f.fields = fields
	if f.cursor >= len(fields) {
		f.cursor = len(fields) - 1
	}
	if f.cursor < 0 {
		f.cursor = 0
	}
	if fetch != "" {
		return tryValuesCmd(m.eng, fetch)
	}
	return nil
}

// urlLabel is the url field's hint. Image stacks can pin the HTTP port with
// host:port (e.g. mailpit:8025); compose/template route by service, so only the
// hostname applies there.
func urlLabel(src string) string {
	if src == "image" {
		return "url (blank=name · host or host:port, e.g. mailpit:8025)"
	}
	return "url (blank = name → <host>.localhost)"
}

// openTryForm opens the Catalog try values form for a template, pre-populated
// from its values.yaml. Must-change placeholders block submit until set.
func (m *Model) openTryForm(tmpl string, vals []engine.TryValue) {
	fields := make([]formField, len(vals))
	for i, v := range vals {
		fields[i] = formField{key: v.Key, label: v.Key, kind: fieldText,
			value: v.Value, mustChange: v.MustChange, orig: v.Value}
	}
	m.form = formState{
		active: true, kind: formTry, title: "try " + tmpl, tmpl: tmpl,
		fields: fields, vals: map[string]string{},
	}
}

// fieldValue returns the current value of the field with the given key.
func (f formState) fieldValue(key string) string {
	for _, fl := range f.fields {
		if fl.key == key {
			return fl.value
		}
	}
	return ""
}

// isPlaceholder reports whether a must-change value is still a placeholder.
func isPlaceholder(v string) bool {
	return strings.TrimSpace(v) == "" || v == "change-me"
}

// splitList tokenizes a free-text list field (image ports/env/volumes) on
// commas and whitespace, dropping empties. Colons in "63799:6379" and '=' in
// "KEY=value" are preserved.
func splitList(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t'
	})
}

// parseHostPort splits the url field into a hostname subdomain and an optional
// HTTP route port: "malpit" → ("malpit", 0); "malpit:8025" → ("malpit", 8025).
// A trailing ".localhost" is tolerated and stripped.
func parseHostPort(s string) (host string, port int) {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, ".localhost")
	if s == "" {
		return "", 0
	}
	if i := strings.LastIndexByte(s, ':'); i >= 0 {
		if p, err := strconv.Atoi(strings.TrimSpace(s[i+1:])); err == nil {
			return strings.TrimSuffix(strings.TrimSpace(s[:i]), ".localhost"), p
		}
	}
	return s, 0
}

// unmet returns the must-change keys still holding a placeholder (formTry).
func (f formState) unmet() []string {
	var out []string
	for _, fl := range f.fields {
		if fl.mustChange && isPlaceholder(fl.value) {
			out = append(out, fl.label)
		}
	}
	return out
}

// sets returns the "key=value" entries whose value differs from the template
// default (formTry).
func (f formState) sets() []string {
	var out []string
	for _, fl := range f.fields {
		if fl.value != fl.orig {
			out = append(out, fl.key+"="+fl.value)
		}
	}
	return out
}

// handleFormKey drives an active input form: Tab/↑↓ move between fields, ←/→ (or
// space) cycle a choice field, Enter submits, Esc aborts, runes/backspace edit a
// text field.
func (m Model) handleFormKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := &m.form
	if len(f.fields) == 0 {
		if msg.Type == tea.KeyEsc || msg.Type == tea.KeyEnter {
			return m.submitForm()
		}
		return m, nil
	}
	cur := &f.fields[f.cursor]
	switch msg.Type {
	case tea.KeyEsc:
		m.form = formState{}
		return m, nil
	case tea.KeyEnter:
		return m.submitForm()
	case tea.KeyTab, tea.KeyDown:
		f.cursor = (f.cursor + 1) % len(f.fields)
		return m, nil
	case tea.KeyShiftTab, tea.KeyUp:
		f.cursor = (f.cursor - 1 + len(f.fields)) % len(f.fields)
		return m, nil
	case tea.KeyLeft:
		if cur.kind == fieldChoice {
			return m.cycleChoice(-1)
		}
		return m, nil
	case tea.KeyRight:
		if cur.kind == fieldChoice {
			return m.cycleChoice(1)
		}
		return m, nil
	case tea.KeySpace:
		if cur.kind == fieldChoice {
			return m.cycleChoice(1)
		}
		cur.value += " "
		m.stickValue(cur.key, cur.value)
		return m, nil
	case tea.KeyBackspace:
		if cur.kind == fieldText && len(cur.value) > 0 {
			cur.value = cur.value[:len(cur.value)-1]
			m.stickValue(cur.key, cur.value)
		}
		return m, nil
	case tea.KeyRunes:
		if cur.kind == fieldChoice {
			return m, nil // typing doesn't edit a cycler
		}
		cur.value += string(msg.Runes)
		m.stickValue(cur.key, cur.value)
		return m, nil
	}
	return m, nil
}

// stickValue records a text edit so it survives create-form rebuilds.
func (m *Model) stickValue(key, val string) {
	if m.form.vals != nil {
		m.form.vals[key] = val
	}
}

// cycleChoice advances the focused template cycler and rebuilds the create
// form's fields, keeping the cursor on the template field.
func (m Model) cycleChoice(delta int) (tea.Model, tea.Cmd) {
	f := &m.form
	if f.fields[f.cursor].key != "template" {
		return m, nil
	}
	if n := len(f.tmplNames); n > 0 {
		f.tmplSel = (f.tmplSel + delta + n) % n
	}
	cmd := m.rebuildCreateFields()
	for i := range f.fields {
		if f.fields[i].key == "template" {
			f.cursor = i
		}
	}
	return m, cmd
}

// submitForm validates and dispatches the active form. Create dispatches per
// source (compose→Add, template→Try named+keep, image→RunImage); try launches
// an ephemeral stack. Validation problems set an inline error and keep the form.
func (m Model) submitForm() (tea.Model, tea.Cmd) {
	switch m.form.kind {
	case formCreate:
		return m.submitCreate()
	case formTry:
		if unmet := m.form.unmet(); len(unmet) > 0 {
			m.form.err = "set required value(s): " + strings.Join(unmet, ", ")
			return m, nil
		}
		return m, tryCmd(m.eng, m.form.tmpl, m.form.sets())
	}
	return m, nil
}

// submitCreate dispatches the adaptive create form for the chosen source.
func (m Model) submitCreate() (tea.Model, tea.Cmd) {
	name := strings.TrimSpace(m.form.vals["name"])
	if name == "" {
		m.form.err = "name is required"
		return m, nil
	}
	// url is "host" or "host:port"; the port pins the HTTP route (image only —
	// compose/template port routing is driven by their service classification).
	host, httpPort := parseHostPort(m.form.vals["url"])
	switch createSources[m.form.srcIdx] {
	case "compose":
		path := strings.TrimSpace(m.form.vals["path"])
		if path == "" {
			m.form.err = "a compose file or directory is required"
			return m, nil
		}
		return m, addCmd(m.eng, name, path, host)
	case "image":
		image := strings.TrimSpace(m.form.vals["image"])
		if image == "" {
			m.form.err = "an image reference is required"
			return m, nil
		}
		ports := splitList(m.form.vals["ports"])
		env := splitList(m.form.vals["env"])
		vols := splitList(m.form.vals["volumes"])
		return m, runImageCmd(m.eng, name, image, ports, env, vols, host, httpPort)
	case "template":
		if len(m.form.tmplNames) == 0 {
			m.form.err = "no templates available — add one with kazi template"
			return m, nil
		}
		tmpl := m.form.tmplNames[m.form.tmplSel]
		var unmet, sets []string
		for _, fl := range m.form.fields {
			if !strings.HasPrefix(fl.key, tvPrefix) {
				continue
			}
			if fl.mustChange && isPlaceholder(fl.value) {
				unmet = append(unmet, fl.label)
			}
			if fl.value != fl.orig {
				sets = append(sets, fl.label+"="+fl.value)
			}
		}
		if len(unmet) > 0 {
			m.form.err = "set required value(s): " + strings.Join(unmet, ", ")
			return m, nil
		}
		return m, createTemplateCmd(m.eng, name, tmpl, sets, host)
	}
	return m, nil
}
