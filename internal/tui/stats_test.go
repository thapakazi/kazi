package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/thapakazi/kazi/internal/engine"
)

// recordStatsEngine captures the ids the Stats tab scopes its stream to.
type recordStatsEngine struct {
	fakeEngine
	ids  []string
	open int
}

func (r *recordStatsEngine) StatsStream(ctx context.Context, ids []string) (<-chan engine.StatSample, error) {
	r.ids = ids
	r.open++
	return r.fakeEngine.StatsStream(ctx, ids)
}

// statsModel parks a loaded model on the Stats tab for "blog", with a status read
// whose containers carry IDs (running web/db, exited cache) so ID scoping is
// observable.
func statsModel(t *testing.T) Model {
	t.Helper()
	m := selectStack(t, loaded(t), "blog")
	m.tab = tabStats
	m.focus = focusDetail
	m.statusName = "blog"
	m.statusInfo = engine.StackInfo{Name: "blog", Containers: []engine.ContainerInfo{
		{Name: "blog-web-1", Service: "web", State: "running", ID: "w1"},
		{Name: "blog-db-1", Service: "db", State: "running", ID: "d1"},
		{Name: "blog-cache-1", Service: "cache", State: "exited", ID: "c1"},
	}}
	return m
}

func TestStatsSyncScopesToRunningIDsAndTearsDown(t *testing.T) {
	m := statsModel(t)
	rec := &recordStatsEngine{}
	m.eng = rec

	// Entering the Stats tab opens a stream scoped to the running container IDs.
	cmd := m.statsSyncCmd()
	if cmd == nil {
		t.Fatal("entering the Stats tab should open a stream")
	}
	if m.statsStack != "blog" || m.statsService != "" {
		t.Fatalf("stream target = %q/%q, want blog/all", m.statsStack, m.statsService)
	}
	msg := cmd() // runs startStatsStreamCmd → records the scoped ids
	if got := strings.Join(rec.ids, ","); got != "w1,d1" {
		t.Fatalf("scoped ids = %q, want running only (w1,d1)", got)
	}
	sm, ok := msg.(statsStreamMsg)
	if !ok {
		t.Fatalf("want statsStreamMsg, got %T", msg)
	}
	nm, _ := m.Update(sm)
	m = nm.(Model)
	if m.statsCancel == nil {
		t.Fatal("model should hold the stream cancel func")
	}

	// Leaving the Stats tab tears the stream down.
	m.tab = tabConfig
	m.statsSyncCmd()
	if m.statsStack != "" || m.statsCancel != nil {
		t.Fatalf("leaving the tab should tear the stream down: %q", m.statsStack)
	}
}

func TestStatsRecordSampleRingBuffer(t *testing.T) {
	m := statsModel(t)
	m.statsStack = "blog"
	m.statsHistory = 3
	for _, cpu := range []float64{1, 2, 3, 4, 5} {
		nm, _ := m.Update(statSampleMsg{stack: "blog", sample: engine.StatSample{
			ContainerStats: engine.ContainerStats{Name: "blog-web-1", CPUPercent: cpu, MemPercent: 10}}})
		m = nm.(Model)
	}
	ser := m.statsSeries["blog-web-1"]
	if ser == nil {
		t.Fatal("sample was not recorded")
	}
	if len(ser.cpu) != 3 {
		t.Fatalf("ring should cap at statsHistory=3, got %d", len(ser.cpu))
	}
	if ser.cpu[0] != 3 || ser.cpu[2] != 5 || ser.latest.CPUPercent != 5 {
		t.Fatalf("ring retained wrong window: %v (latest %v)", ser.cpu, ser.latest.CPUPercent)
	}
}

func TestStatsStaleSampleDropped(t *testing.T) {
	m := statsModel(t)
	m.statsStack = "blog"
	nm, _ := m.Update(statSampleMsg{stack: "redis", sample: engine.StatSample{
		ContainerStats: engine.ContainerStats{Name: "redis-1", CPUPercent: 9}}})
	m = nm.(Model)
	if _, ok := m.statsSeries["redis-1"]; ok {
		t.Fatal("a sample from a stale stream must be dropped")
	}
}

func TestStatsContainerFilterScopesStream(t *testing.T) {
	m := statsModel(t)
	m.statsStack = "blog"
	rec := &recordStatsEngine{}
	m.eng = rec

	// c opens the container picker: "all services" + each service (sorted).
	m = press(m, keyRunes("c"))
	if !m.modal.active || m.modal.mkind != modalStatsService {
		t.Fatalf("c should open the stats container picker, got %+v", m.modal)
	}
	if got := strings.Join(m.modal.options, ","); got != "all services,cache,db,web" {
		t.Fatalf("picker options = %q", got)
	}

	// Select "db" (option 3) → the stream restarts scoped to that service's id.
	nm, cmd := m.Update(keyRunes("3"))
	m = nm.(Model)
	if m.statsService != "db" {
		t.Fatalf("statsService = %q, want db", m.statsService)
	}
	if cmd == nil {
		t.Fatal("choosing a service should restart the stream")
	}
	cmd()
	if got := strings.Join(rec.ids, ","); got != "d1" {
		t.Fatalf("scoped ids = %q, want just db (d1)", got)
	}
}

func TestStatsRuntimeUnavailableDegrades(t *testing.T) {
	m := statsModel(t)
	m.statsStack = "blog"
	nm, _ := m.Update(statsErrMsg{stack: "blog", err: errors.New("no json stats")})
	m = nm.(Model)
	if !strings.Contains(m.statsErr, "stats unavailable") {
		t.Fatalf("statsErr = %q, want an unavailable notice", m.statsErr)
	}
	body := m.renderStats(engine.StackInfo{Name: "blog"}, 80)
	if !strings.Contains(body, "stats unavailable") {
		t.Fatalf("Stats tab should show the notice, got %q", body)
	}
}

func TestStatsRenderShowsSparklinesAndExitedDash(t *testing.T) {
	m := statsModel(t)
	m.statsStack = "blog"
	// Two rising CPU samples for web; db running but no sample; cache exited.
	for _, cpu := range []float64{1, 3} {
		nm, _ := m.Update(statSampleMsg{stack: "blog", sample: engine.StatSample{
			ContainerStats: engine.ContainerStats{Name: "blog-web-1", CPUPercent: cpu, MemPercent: 25,
				MemUsage: "128MiB", MemLimit: "512MiB", NetRx: "1.2MB", NetTx: "340kB",
				BlockRead: "45MB", BlockWrite: "12MB", PIDs: 14}}})
		m = nm.(Model)
	}
	body := m.renderStats(m.statusInfo, 120)
	if !strings.Contains(body, "web") || !strings.Contains(body, "PIDs 14") {
		t.Fatalf("web row missing stats: %q", body)
	}
	if !strings.Contains(body, "streaming…") {
		t.Fatalf("db (running, no sample yet) should show streaming…: %q", body)
	}
	if !strings.Contains(body, "cache    —") {
		t.Fatalf("exited cache should show —, not zero: %q", body)
	}
}

func TestHostOverviewRendersGraphsAndAggregate(t *testing.T) {
	m := loaded(t) // ALL selected by default
	nm, _ := m.Update(hostStatsMsg{
		hs:        engine.HostStats{CPUPercent: 312, CPUCores: 14, MemUsed: 12 << 30, MemTotal: 36 << 30, DiskUsed: 884 << 30, DiskTotal: 994 << 30},
		aggCPU:    4.7,
		aggMem:    1932735283, // ~1.8G
		aggStacks: 11,
	})
	m = nm.(Model)
	if !m.hostHave {
		t.Fatal("hostStatsMsg should mark host data present")
	}
	ov := m.renderOverview()
	for _, want := range []string{"CPU (14 cores)", "312%", "Memory (36.0G)", "Disk", "aggregate containers", "across 11 stacks"} {
		if !strings.Contains(ov, want) {
			t.Fatalf("overview missing %q:\n%s", want, ov)
		}
	}
}

func TestHostOverviewDegradesMissingDisk(t *testing.T) {
	m := loaded(t)
	nm, _ := m.Update(hostStatsMsg{hs: engine.HostStats{CPUPercent: 10, CPUCores: 2, MemUsed: 1, MemTotal: 2}})
	m = nm.(Model)
	ov := m.renderOverview()
	if !strings.Contains(ov, "Disk") || !strings.Contains(ov, "—") {
		t.Fatalf("missing disk should degrade to —:\n%s", ov)
	}
}

func TestOnAllOverviewTriggersHostPoll(t *testing.T) {
	m := loaded(t)
	if !m.onAllOverview() {
		t.Fatal("default selection ALL should be the host overview")
	}
	// Selecting a stack leaves the host overview.
	m = selectStack(t, m, "blog")
	if m.onAllOverview() {
		t.Fatal("a stack selection is not the host overview")
	}
}
