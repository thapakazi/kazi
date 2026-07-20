package tui

import (
	"context"
	"fmt"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/thapakazi/kazi/internal/engine"
	"github.com/thapakazi/kazi/internal/store"
)

// m6RecEngine records the M6 write calls for assertions.
type m6RecEngine struct {
	fakeEngine
	sets        []string
	tryName     string
	imgName     string
	image       string
	imgPort     []string
	imgEnv      []string
	imgVol      []string
	imgHost     string // image RunOpts.Hostname
	imgHTTPPort int    // image RunOpts.HTTPPort
	hostName    string // SetHostname target (compose/template path)
	hostHost    string // SetHostname value
	rmContainer string // RemoveContainer target
	routeAdds   []string
	kept        []string
	torn        []string
	added       []string
}

func (e *m6RecEngine) SetHostname(name, host string) error {
	e.hostName, e.hostHost = name, host
	return nil
}

func (e *m6RecEngine) Add(name, path string) (store.Manifest, error) {
	e.added = append(e.added, name+"="+path)
	m := store.Manifest{APIVersion: "kazi.dev/v1alpha1", Kind: "Stack"}
	m.Metadata.Name = name
	return m, nil
}
func (e *m6RecEngine) Try(_ context.Context, tmpl string, opts engine.TryOpts) (string, []engine.Endpoint, error) {
	e.sets = opts.Sets
	e.tryName = opts.Name
	name := opts.Name
	if name == "" {
		name = tmpl
	}
	return name, nil, nil
}
func (e *m6RecEngine) RunImage(_ context.Context, name, image string, opts engine.RunOpts) (string, error) {
	e.imgName, e.image = name, image
	e.imgPort, e.imgEnv, e.imgVol = opts.Ports, opts.Envs, opts.Vols
	e.imgHost, e.imgHTTPPort = opts.Hostname, opts.HTTPPort
	return name, nil
}
func (e *m6RecEngine) RemoveContainer(_ context.Context, name string) error {
	e.rmContainer = name
	return nil
}
func (e *m6RecEngine) RouteAdd(_ context.Context, host string, port int, _, _ string) error {
	e.routeAdds = append(e.routeAdds, fmt.Sprintf("%s=%d", host, port))
	return nil
}
func (e *m6RecEngine) Keep(name string) error { e.kept = append(e.kept, name); return nil }
func (e *m6RecEngine) Teardown(_ context.Context, name string) error {
	e.torn = append(e.torn, name)
	return nil
}

// adoptRecEngine records Adopt calls for the a:adopt test.
type adoptRecEngine struct {
	fakeEngine
	adopted *[]string
}

func (e adoptRecEngine) Adopt(_ context.Context, name string, _ []string) error {
	*e.adopted = append(*e.adopted, name)
	return nil
}

// withTemplates applies the catalog list a real Init would have loaded.
func withTemplates(m Model) Model {
	nm, _ := m.Update(templatesCmd(m.eng)())
	return nm.(Model)
}

// --- Create form (n) --------------------------------------------------------

func TestSourceChooserOpensOnN(t *testing.T) {
	m := withTemplates(loaded(t))
	m = press(m, keyRunes("n"))
	if !m.modal.active || m.modal.mkind != modalSourceChoose {
		t.Fatalf("n should open the transient source chooser, got %+v", m.modal)
	}
	view := m.View()
	for _, s := range []string{"compose", "template", "image"} {
		if !contains(view, s) {
			t.Fatalf("chooser should list %q, got:\n%s", s, view)
		}
	}
	// esc cancels the chooser without opening a form.
	m = press(m, special(tea.KeyEsc))
	if m.modal.active || m.form.active {
		t.Fatal("esc should close the chooser and open nothing")
	}
}

func TestComposeFormFromChooser(t *testing.T) {
	m := withTemplates(loaded(t))
	m, _ = chooseSource(m, "c")
	if !m.form.active || m.form.kind != formCreate {
		t.Fatalf("c should open the compose form, got %+v", m.form)
	}
	// Compose form: name + url + path — no source cycler row.
	if len(m.form.fields) != 3 || m.form.fields[0].key != "name" ||
		m.form.fields[1].key != "url" || m.form.fields[2].key != "path" {
		t.Fatalf("compose form should be name+url+path, got %+v", m.form.fields)
	}
	if !contains(m.View(), "compose file or directory") {
		t.Fatalf("compose form should show the path field, got:\n%s", m.View())
	}
}

func TestCreateFormImageCustomHostnameAndPort(t *testing.T) {
	rec := &m6RecEngine{}
	m := withTemplates(loadedWith(t, rec))
	m, _ = chooseSource(m, "i")
	m = typeInto(m, "name", "mail")
	m = typeInto(m, "url", "mailpit:8025") // host:port pins the HTTP route
	m = typeInto(m, "image", "axllent/mailpit")
	nm, cmd := m.Update(special(tea.KeyEnter))
	m = nm.(Model)
	nm, _ = m.Update(cmd())
	m = nm.(Model)
	if rec.imgName != "mail" || rec.imgHost != "mailpit" || rec.imgHTTPPort != 8025 {
		t.Fatalf("RunImage opts = (name=%q host=%q port=%d), want (mail, mailpit, 8025)",
			rec.imgName, rec.imgHost, rec.imgHTTPPort)
	}
}

func TestCreateFormImageHostnameDefaults(t *testing.T) {
	rec := &m6RecEngine{}
	m := withTemplates(loadedWith(t, rec))
	m, _ = chooseSource(m, "i")
	m = typeInto(m, "name", "cache")
	m = typeInto(m, "image", "redis:7")
	// url blank → no custom hostname/port (defaults to the stack name, auto-port).
	nm, cmd := m.Update(special(tea.KeyEnter))
	m = nm.(Model)
	nm, _ = m.Update(cmd())
	m = nm.(Model)
	if rec.imgHost != "" || rec.imgHTTPPort != 0 {
		t.Fatalf("blank url should not set a route, got host=%q port=%d", rec.imgHost, rec.imgHTTPPort)
	}
}

func TestParseHostPort(t *testing.T) {
	cases := []struct {
		in   string
		host string
		port int
	}{
		{"malpit", "malpit", 0},
		{"malpit:8025", "malpit", 8025},
		{"malpit.localhost", "malpit", 0},
		{"malpit.localhost:8025", "malpit", 8025},
		{"", "", 0},
		{"  ", "", 0},
	}
	for _, tc := range cases {
		host, port := parseHostPort(tc.in)
		if host != tc.host || port != tc.port {
			t.Errorf("parseHostPort(%q) = (%q,%d), want (%q,%d)", tc.in, host, port, tc.host, tc.port)
		}
	}
}

// chooseSource opens the new-stack chooser (n) and picks a source (c/t/i),
// returning the model and any command the pick produced (e.g. a template fetch).
func chooseSource(m Model, key string) (Model, tea.Cmd) {
	m = press(m, keyRunes("n"))
	nm, cmd := m.Update(keyRunes(key))
	return nm.(Model), cmd
}

// typeInto focuses the field with the given key, clears it, and types text.
func typeInto(m Model, key, text string) Model {
	for i := range m.form.fields {
		if m.form.fields[i].key == key {
			m.form.cursor = i
			m.form.fields[i].value = ""
			if m.form.vals != nil {
				m.form.vals[key] = ""
			}
		}
	}
	for _, r := range text {
		m = press(m, keyRunes(string(r)))
	}
	return m
}

func TestCreateFormComposeDispatchesAdd(t *testing.T) {
	rec := &m6RecEngine{}
	m := withTemplates(loadedWith(t, rec))
	m, _ = chooseSource(m, "c")
	m = typeInto(m, "name", "newblog")
	m = typeInto(m, "path", "/tmp/x/compose.yaml")
	nm, cmd := m.Update(special(tea.KeyEnter))
	m = nm.(Model)
	if cmd == nil {
		t.Fatal("submitting the create form produced no command")
	}
	nm, _ = m.Update(cmd())
	m = nm.(Model)
	if len(rec.added) != 1 || rec.added[0] != "newblog=/tmp/x/compose.yaml" {
		t.Fatalf("Add calls = %v, want [newblog=/tmp/x/compose.yaml]", rec.added)
	}
	if m.form.active {
		t.Fatal("form should close after a successful create")
	}
	if m.pendingSelect != "newblog" {
		t.Fatalf("new stack should be pending-selected, got %q", m.pendingSelect)
	}
}

func TestCreateFormComposeCustomHostname(t *testing.T) {
	rec := &m6RecEngine{}
	m := withTemplates(loadedWith(t, rec))
	m, _ = chooseSource(m, "c")
	m = typeInto(m, "name", "blog")
	m = typeInto(m, "url", "www") // compose hostname via post-create SetHostname
	m = typeInto(m, "path", "/x/compose.yaml")
	nm, cmd := m.Update(special(tea.KeyEnter))
	m = nm.(Model)
	nm, _ = m.Update(cmd())
	m = nm.(Model)
	if rec.hostName != "blog" || rec.hostHost != "www" {
		t.Fatalf("compose SetHostname = (%q,%q), want (blog,www)", rec.hostName, rec.hostHost)
	}
}

func TestCreateFormImageSourceRunsImage(t *testing.T) {
	rec := &m6RecEngine{}
	m := withTemplates(loadedWith(t, rec))
	m, _ = chooseSource(m, "i")
	if createSources[m.form.srcIdx] != "image" {
		t.Fatalf("i should open the image form, got %q", createSources[m.form.srcIdx])
	}
	m = typeInto(m, "name", "cache")
	m = typeInto(m, "image", "redis:7")
	m = typeInto(m, "ports", "63799:6379")
	nm, cmd := m.Update(special(tea.KeyEnter))
	m = nm.(Model)
	nm, _ = m.Update(cmd())
	m = nm.(Model)
	if rec.imgName != "cache" || rec.image != "redis:7" {
		t.Fatalf("RunImage = (%q,%q), want (cache,redis:7)", rec.imgName, rec.image)
	}
	if len(rec.imgPort) != 1 || rec.imgPort[0] != "63799:6379" {
		t.Fatalf("RunImage ports = %v, want [63799:6379]", rec.imgPort)
	}
	if m.pendingSelect != "cache" {
		t.Fatalf("new image stack should be pending-selected, got %q", m.pendingSelect)
	}
}

func TestSplitList(t *testing.T) {
	cases := map[string][]string{
		"63799:6379":              {"63799:6379"},
		"8080:80, 5432:5432":      {"8080:80", "5432:5432"},
		"A=1 B=2":                 {"A=1", "B=2"},
		"  ":                      nil,
		"data:/data,cfg:/etc/cfg": {"data:/data", "cfg:/etc/cfg"},
	}
	for in, want := range cases {
		got := splitList(in)
		if len(got) != len(want) {
			t.Fatalf("splitList(%q) = %v, want %v", in, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("splitList(%q)[%d] = %q, want %q", in, i, got[i], want[i])
			}
		}
	}
}

func TestCreateFormTemplateSourceFetchesValuesAndCreates(t *testing.T) {
	rec := &m6RecEngine{}
	m := withTemplates(loadedWith(t, rec))
	// t opens the template form and triggers a lazy values fetch.
	m, cmd := chooseSource(m, "t")
	if createSources[m.form.srcIdx] != "template" {
		t.Fatalf("t should open the template form, got %q", createSources[m.form.srcIdx])
	}
	if cmd == nil {
		t.Fatal("opening the template form should fetch its values")
	}
	nm, _ := m.Update(cmd()) // tryValuesMsg → cache + rebuild
	m = nm.(Model)
	// Value fields now present: postgres_db, postgres_password (must-change).
	if m.form.fieldValue(tvPrefix+"postgres_password") != "change-me" {
		t.Fatalf("template values should populate, fields=%+v", m.form.fields)
	}
	// Submit blocked until the must-change password is set.
	nm, cmd = m.Update(special(tea.KeyEnter))
	m = nm.(Model)
	if cmd != nil || !m.form.active {
		t.Fatal("must-change placeholder should block the template create")
	}
	// Fill name + password, then submit.
	m = typeInto(m, "name", "cache")
	m = typeInto(m, tvPrefix+"postgres_password", "s3cret")
	nm, cmd = m.Update(special(tea.KeyEnter))
	m = nm.(Model)
	if cmd == nil {
		t.Fatal("valid template create should dispatch")
	}
	nm, _ = m.Update(cmd())
	m = nm.(Model)
	if rec.tryName != "cache" {
		t.Fatalf("template create should name the stack, got %q", rec.tryName)
	}
	if len(rec.sets) != 1 || rec.sets[0] != "postgres_password=s3cret" {
		t.Fatalf("template create --set = %v, want [postgres_password=s3cret]", rec.sets)
	}
	if m.pendingSelect != "cache" {
		t.Fatalf("new template stack should be pending-selected, got %q", m.pendingSelect)
	}
}

func TestCreateFormInlineValidation(t *testing.T) {
	m := withTemplates(loaded(t))
	m, _ = chooseSource(m, "c")
	// Submit with empty fields → inline error, form stays open, no dispatch.
	nm, cmd := m.Update(special(tea.KeyEnter))
	m = nm.(Model)
	if !m.form.active {
		t.Fatal("empty submit should keep the form open")
	}
	if m.form.err == "" {
		t.Fatal("empty submit should set an inline error")
	}
	if cmd != nil {
		t.Fatal("empty submit should not dispatch a command")
	}
}

func TestCreateFormEscAborts(t *testing.T) {
	m := withTemplates(loaded(t))
	m, _ = chooseSource(m, "c")
	m = press(m, special(tea.KeyEsc))
	if m.form.active {
		t.Fatal("Esc should close the create form")
	}
}

// --- Remove transient (d) ---------------------------------------------------

func TestRemoveTransientDiscoveredDown(t *testing.T) {
	var calls []string
	m := loadedWith(t, lifecycleEngine{calls: &calls})
	m = selectStack(t, m, "redis") // discovered in the fixture
	m = press(m, keyRunes("d"))
	if !m.modal.active || m.modal.mkind != modalRemoveChoose {
		t.Fatalf("d should open the remove transient, got %+v", m.modal)
	}
	// Discovered stacks have no manifest → only d (down & remove) is offered.
	if contains(m.View(), "deregister") {
		t.Fatal("a discovered stack should not offer deregister")
	}
	nm, cmd := m.Update(keyRunes("d"))
	m = nm.(Model)
	if m.modal.active {
		t.Fatal("choosing should close the transient")
	}
	if cmd == nil {
		t.Fatal("dd produced no command")
	}
	cmd() // runs ActionStream("down", redis)
	if len(calls) != 1 || calls[0] != "down:redis" {
		t.Fatalf("calls = %v, want [down:redis]", calls)
	}
}

func TestRemoveTransientRegisteredDeregister(t *testing.T) {
	var removed []string
	m := loadedWith(t, recordingEngine{removed: &removed})
	m = selectStack(t, m, "blog") // registered
	m = press(m, keyRunes("d"))
	if !m.modal.active || m.modal.mkind != modalRemoveChoose {
		t.Fatalf("d should open the remove transient, got %+v", m.modal)
	}
	if !contains(m.View(), "deregister") {
		t.Fatal("a registered stack should offer deregister")
	}
	nm, cmd := m.Update(keyRunes("r"))
	m = nm.(Model)
	if m.modal.active {
		t.Fatal("r should close the transient")
	}
	cmd() // runs Remove
	if len(removed) != 1 || removed[0] != "blog" {
		t.Fatalf("Remove calls = %v, want [blog]", removed)
	}
}

func TestRemoveTransientRegisteredDown(t *testing.T) {
	var calls []string
	m := loadedWith(t, lifecycleEngine{calls: &calls})
	m = selectStack(t, m, "blog")
	m = press(m, keyRunes("d"))
	nm, cmd := m.Update(keyRunes("d"))
	m = nm.(Model)
	cmd()
	if len(calls) != 1 || calls[0] != "down:blog" {
		t.Fatalf("dd on a registered stack should down it, calls = %v", calls)
	}
}

func TestRemoveTransientDiscoveredDeregisterInert(t *testing.T) {
	m := selectStack(t, loaded(t), "redis") // discovered
	m = press(m, keyRunes("d"))
	nm, cmd := m.Update(keyRunes("r"))
	m = nm.(Model)
	if !m.modal.active {
		t.Fatal("r on a discovered stack should be inert (no manifest to deregister)")
	}
	if cmd != nil {
		t.Fatal("r should dispatch nothing on a discovered stack")
	}
}

func TestRemoveTransientUnmanagedRemovesContainer(t *testing.T) {
	rec := &m6RecEngine{}
	m := loadedWith(t, rec)
	m = selectStack(t, m, "n8n") // unmanaged loose container in the fixture
	if m.selectedRow().selKind != selUnmanaged {
		t.Fatalf("n8n should be unmanaged, got %v", m.selectedRow().selKind)
	}
	m = press(m, keyRunes("d"))
	if !m.modal.active || m.modal.mkind != modalRemoveChoose {
		t.Fatalf("d should open the remove transient on an unmanaged row, got %+v", m.modal)
	}
	// Unmanaged offers container removal, not deregister/compose-down wording.
	if !contains(m.View(), "docker rm -f") || contains(m.View(), "deregister") {
		t.Fatalf("unmanaged transient should offer rm -f only, got:\n%s", m.View())
	}
	nm, cmd := m.Update(keyRunes("d"))
	m = nm.(Model)
	if m.modal.active {
		t.Fatal("dd should close the transient")
	}
	cmd() // runs RemoveContainer
	if rec.rmContainer != "n8n" {
		t.Fatalf("RemoveContainer target = %q, want n8n", rec.rmContainer)
	}
}

func TestAdoptOnUnmanaged(t *testing.T) {
	var adopted []string
	eng := adoptRecEngine{adopted: &adopted}
	m := selectStack(t, loadedWith(t, eng), "n8n") // unmanaged loose container
	nm, cmd := m.Update(keyRunes("a"))
	m = nm.(Model)
	if cmd == nil {
		t.Fatal("a on an unmanaged row should dispatch adopt")
	}
	cmd()
	if len(adopted) != 1 || adopted[0] != "n8n" {
		t.Fatalf("Adopt calls = %v, want [n8n]", adopted)
	}
}

func TestAdoptInertOnStack(t *testing.T) {
	m := selectStack(t, loaded(t), "blog") // registered, not unmanaged
	nm, cmd := m.Update(keyRunes("a"))
	m = nm.(Model)
	if cmd != nil {
		t.Fatal("a should be inert on a non-unmanaged row")
	}
}

func TestActionPanelToggleOnBacktick(t *testing.T) {
	m := sized(t, 120, 40)
	m.actionTitle = "up blog"
	m.actionOpen = true
	// a no longer toggles the panel (it's a:adopt now).
	m2 := press(m, keyRunes("a"))
	if !m2.actionOpen {
		t.Fatal("a must not toggle the action panel anymore")
	}
	// ` toggles it.
	m3 := press(m, keyRunes("`"))
	if m3.actionOpen {
		t.Fatal("` should toggle the action panel")
	}
}

func TestStackMenuRouteAddsPublishedPorts(t *testing.T) {
	rec := &m6RecEngine{}
	m := loadedWith(t, rec)
	m = selectStack(t, m, "redis") // running discovered → fake suggests one route
	m = press(m, keyRunes("s"))
	ri := -1
	for i, v := range m.modal.values {
		if v == "route" {
			ri = i
		}
	}
	if ri < 0 {
		t.Fatalf("running stack menu should offer route, got %v", m.modal.values)
	}
	nm, cmd := m.Update(keyRunes(string(rune('1' + ri))))
	m = nm.(Model)
	if cmd == nil {
		t.Fatal("choosing route produced no command")
	}
	nm, _ = m.Update(cmd())
	m = nm.(Model)
	if len(rec.routeAdds) != 1 || rec.routeAdds[0] != "redis=63799" {
		t.Fatalf("RouteAdd calls = %v, want [redis=63799]", rec.routeAdds)
	}
	if m.toast == "" {
		t.Fatal("routing should raise a toast")
	}
}

func TestRemoveTransientEscCancels(t *testing.T) {
	m := selectStack(t, loaded(t), "blog")
	m = press(m, keyRunes("d"))
	m = press(m, special(tea.KeyEsc))
	if m.modal.active {
		t.Fatal("esc should close the remove transient")
	}
}

// --- Try form (t) -----------------------------------------------------------

func TestTryFormOpensWithValues(t *testing.T) {
	m := withTemplates(loaded(t))
	m = press(m, keyRunes("2")) // Catalog mode
	nm, cmd := m.Update(keyRunes("t"))
	m = nm.(Model)
	if cmd == nil {
		t.Fatal("t should load template values")
	}
	nm, _ = m.Update(cmd()) // tryValuesMsg → openTryForm
	m = nm.(Model)
	if !m.form.active || m.form.kind != formTry {
		t.Fatalf("t should open the try form, got %+v", m.form)
	}
	if len(m.form.fields) != 2 {
		t.Fatalf("try form should list 2 values, got %d", len(m.form.fields))
	}
}

func TestTryFormMustChangeBlocksThenComposesSets(t *testing.T) {
	rec := &m6RecEngine{}
	m := withTemplates(loadedWith(t, rec))
	m = press(m, keyRunes("2")) // Catalog mode
	m = press(m, keyRunes("j")) // move to the second template (postgres)
	nm, cmd := m.Update(keyRunes("t"))
	m = nm.(Model)
	nm, _ = m.Update(cmd())
	m = nm.(Model)

	// Submit with the password still "change-me" → blocked, named in the error.
	nm, cmd = m.Update(special(tea.KeyEnter))
	m = nm.(Model)
	if !m.form.active || cmd != nil {
		t.Fatal("must-change placeholder should block submit")
	}
	if m.form.err == "" || !contains(m.form.err, "postgres_password") {
		t.Fatalf("blocked submit should name the unmet key, got %q", m.form.err)
	}

	// Set the password, leave db at its default, submit → one --set for password.
	for i := range m.form.fields {
		if m.form.fields[i].key == "postgres_password" {
			m.form.fields[i].value = "s3cret"
		}
	}
	nm, cmd = m.Update(special(tea.KeyEnter))
	m = nm.(Model)
	if cmd == nil {
		t.Fatal("valid try submit should dispatch")
	}
	nm, _ = m.Update(cmd())
	m = nm.(Model)
	if len(rec.sets) != 1 || rec.sets[0] != "postgres_password=s3cret" {
		t.Fatalf("try --set = %v, want [postgres_password=s3cret]", rec.sets)
	}
	if m.watchStack != "postgres" {
		t.Fatalf("try should watch the new ephemeral stack, got %q", m.watchStack)
	}
}

// --- Open-in-editor flow (o → e) --------------------------------------------

// captureEditor swaps the editor launcher for a recorder for the duration of a
// test, returning the slice of paths it was asked to open. The recorded path is
// captured when the command is built (during Update), independent of the real
// $EDITOR and of whether it would detach or suspend.
func captureEditor(t *testing.T) *[]string {
	t.Helper()
	var opened []string
	prev := editorOpen
	editorOpen = func(p string) tea.Cmd {
		opened = append(opened, p)
		return func() tea.Msg { return editorOpenedMsg{path: p} }
	}
	t.Cleanup(func() { editorOpen = prev })
	return &opened
}

// TestOpenMenuOffersEditor: o opens the transient open menu; e resolves the
// stack's edit targets.
func TestOpenMenuOffersEditor(t *testing.T) {
	m := selectStack(t, loaded(t), "blog")
	m = press(m, keyRunes("o"))
	if !m.modal.active || m.modal.mkind != modalOpenChoose {
		t.Fatalf("o should open the open menu, got %+v", m.modal)
	}
	nm, cmd := m.Update(keyRunes("e"))
	m = nm.(Model)
	if cmd == nil {
		t.Fatal("o-e should resolve edit targets")
	}
}

// TestOpenEditorPickerForComposeStack: o-e on a compose-backed stack offers a
// config-vs-project picker (manifest file + compose directory).
func TestOpenEditorPickerForComposeStack(t *testing.T) {
	m := selectStack(t, loaded(t), "blog") // fake gives blog manifest+compose
	m = press(m, keyRunes("o"))
	nm, cmd := m.Update(keyRunes("e"))
	m = nm.(Model)
	nm, _ = m.Update(cmd()) // editTargetsMsg with 2 targets → picker
	m = nm.(Model)
	if !m.modal.active || m.modal.mkind != modalEditOpen {
		t.Fatalf("compose-backed stack should open the config/project picker, got %+v", m.modal)
	}
	if len(m.modal.values) != 2 {
		t.Fatalf("picker should offer config+project, got %v", m.modal.values)
	}
	// config is the manifest file; project is the compose file's directory.
	if m.modal.values[0] != "/cfg/stacks/blog.yaml" || m.modal.values[1] != "/tmp/blog" {
		t.Fatalf("picker paths = %v, want [/cfg/stacks/blog.yaml /tmp/blog]", m.modal.values)
	}
}

// TestOpenEditorDirectForConfigOnly: o-e on a manifest-only stack launches the
// detached editor on the config file directly, skipping the picker.
func TestOpenEditorDirectForConfigOnly(t *testing.T) {
	opened := captureEditor(t)
	m := selectStack(t, loaded(t), "api") // manifest only (not compose-backed)
	m = press(m, keyRunes("o"))
	nm, cmd := m.Update(keyRunes("e"))
	m = nm.(Model)
	nm, openC := m.Update(cmd()) // single target → open directly
	m = nm.(Model)
	if m.modal.active {
		t.Fatalf("single target should skip the picker, got %+v", m.modal)
	}
	if openC == nil {
		t.Fatal("single target should dispatch a detached editor open")
	}
	openC() // runs editorOpen
	if len(*opened) != 1 || (*opened)[0] != "/cfg/stacks/api.yaml" {
		t.Fatalf("opened = %v, want [/cfg/stacks/api.yaml]", *opened)
	}
}

// TestOpenEditorChooseProject: picking "project" from the o-e picker launches
// the detached editor on the compose directory; the TUI is never suspended.
func TestOpenEditorChooseProject(t *testing.T) {
	opened := captureEditor(t)
	m := selectStack(t, loaded(t), "blog")
	m = press(m, keyRunes("o"))
	nm, cmd := m.Update(keyRunes("e"))
	m = nm.(Model)
	nm, _ = m.Update(cmd()) // → modalEditOpen picker
	m = nm.(Model)
	m = press(m, keyRunes("j")) // move to "project"
	nm, openC := m.Update(keyRunes("enter"))
	m = nm.(Model)
	if m.modal.active {
		t.Fatal("enter should close the picker")
	}
	openC() // runs editorOpen
	if len(*opened) != 1 || (*opened)[0] != "/tmp/blog" {
		t.Fatalf("opened = %v, want [/tmp/blog]", *opened)
	}
}

// --- Watched ephemeral: keep / gc -------------------------------------------

func TestKeepOnWatchedDispatches(t *testing.T) {
	rec := &m6RecEngine{}
	m := loadedWith(t, rec)
	m.watchStack = "blog"
	m = selectStack(t, m, "blog")

	m = press(m, keyRunes("k")) // guarded: opens confirm
	if !m.modal.active || m.modal.action != actKeep {
		t.Fatalf("k on a watched stack should open a keep confirm, got %+v", m.modal)
	}
	nm, cmd := m.Update(keyRunes("y"))
	m = nm.(Model)
	if cmd == nil {
		t.Fatal("confirming keep produced no command")
	}
	nm, _ = m.Update(cmd())
	m = nm.(Model)
	if len(rec.kept) != 1 || rec.kept[0] != "blog" {
		t.Fatalf("Keep calls = %v, want [blog]", rec.kept)
	}
	if m.watchStack != "" {
		t.Fatal("keeping a watched stack should stop watching it")
	}
}

func TestGcOnWatchedDispatches(t *testing.T) {
	rec := &m6RecEngine{}
	m := loadedWith(t, rec)
	m.watchStack = "blog"
	m = selectStack(t, m, "blog")

	m = press(m, keyRunes("g")) // guarded: opens confirm (not top-motion here)
	if !m.modal.active || m.modal.action != actGc {
		t.Fatalf("g on a watched stack should open a gc confirm, got %+v", m.modal)
	}
	nm, cmd := m.Update(keyRunes("y"))
	m = nm.(Model)
	nm, _ = m.Update(cmd())
	m = nm.(Model)
	if len(rec.torn) != 1 || rec.torn[0] != "blog" {
		t.Fatalf("Teardown calls = %v, want [blog]", rec.torn)
	}
}

func TestKeepInertWhenNotWatched(t *testing.T) {
	m := selectStack(t, loaded(t), "blog") // not watched
	m = press(m, keyRunes("k"))            // k is Up-motion when not watched
	if m.modal.active {
		t.Fatal("k must not open a keep modal on a non-watched stack")
	}
}

func contains(s, sub string) bool { return len(s) >= len(sub) && stringIndex(s, sub) >= 0 }

func stringIndex(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
