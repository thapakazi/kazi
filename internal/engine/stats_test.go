package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/thapakazi/kazi/internal/runtime"
)

func TestParseStatsSplitStringForms(t *testing.T) {
	// Two docker-stats JSON lines exercising the split columns docker emits.
	out := []byte(`{"ID":"abc123","Name":"blog-web-1","CPUPerc":"2.43%","MemUsage":"128MiB / 512MiB","MemPerc":"25.00%","NetIO":"1.2MB / 340kB","BlockIO":"45MB / 12MB","PIDs":"14"}
{"ID":"def456","Name":"blog-db-1","CPUPerc":"0.60%","MemUsage":"201MiB / 512MiB","MemPerc":"39.00%","NetIO":"88kB / 12kB","BlockIO":"6.7GB / 0B","PIDs":"20"}`)

	cs, err := parseStats(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 2 {
		t.Fatalf("got %d rows, want 2: %+v", len(cs), cs)
	}
	web := cs[0]
	if web.Name != "blog-web-1" || web.CPUPercent != 2.43 || web.MemPercent != 25 || web.PIDs != 14 {
		t.Errorf("web scalars = %+v", web)
	}
	if web.MemUsage != "128MiB" || web.MemLimit != "512MiB" {
		t.Errorf("mem split = %q / %q", web.MemUsage, web.MemLimit)
	}
	if web.NetRx != "1.2MB" || web.NetTx != "340kB" {
		t.Errorf("net split = %q / %q", web.NetRx, web.NetTx)
	}
	if web.BlockRead != "45MB" || web.BlockWrite != "12MB" {
		t.Errorf("block split = %q / %q", web.BlockRead, web.BlockWrite)
	}
	if cs[1].BlockRead != "6.7GB" || cs[1].BlockWrite != "0B" {
		t.Errorf("db block split = %q / %q", cs[1].BlockRead, cs[1].BlockWrite)
	}
}

func TestParseStatsSkipsBlankLinesAndReportsBadJSON(t *testing.T) {
	if _, err := parseStats([]byte("\n\n")); err != nil {
		t.Fatalf("blank-only should parse to nothing, got %v", err)
	}
	if _, err := parseStats([]byte("{not json}")); err == nil {
		t.Fatal("expected error on malformed JSON")
	}
}

func TestStatsScopesToRunningContainersAndAttachesStackService(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	blogDir := registerStack(t, "blog")

	f := &runtime.Fake{
		Containers: []runtime.Container{
			container("blog-web-1", "kazi-blog", blogDir, "web", "running", "Up 1 hour"),
			container("blog-db-1", "kazi-blog", blogDir, "db", "exited", "Exited (0)"),
		},
		StatsOut: `{"ID":"abc","Name":"blog-web-1","CPUPerc":"2.43%","MemUsage":"128MiB / 512MiB","MemPerc":"25.00%","NetIO":"1.2MB / 340kB","BlockIO":"45MB / 12MB","PIDs":"14"}`,
	}
	e := testEngine(t, f)

	cs, err := e.Stats(t.Context(), "blog")
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 1 {
		t.Fatalf("want 1 running container, got %d: %+v", len(cs), cs)
	}
	if cs[0].Stack != "blog" || cs[0].Service != "web" {
		t.Errorf("attach = stack %q service %q", cs[0].Stack, cs[0].Service)
	}
	// Only the running container's ID should have been passed to stats.
	if len(f.StatsCalls) != 1 {
		t.Fatalf("want 1 stats call, got %v", f.StatsCalls)
	}
	call := f.StatsCalls[0]
	if call[0] != "nostream" {
		t.Errorf("want one-shot (nostream), got %q", call[0])
	}
	if len(call) != 2 || call[1] != "blog-web-1-id" {
		t.Errorf("want only running container id, got %v", call[1:])
	}
}

func TestStatsUnknownStackIsNotFound(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	f := &runtime.Fake{}
	e := testEngine(t, f)
	if _, err := e.Stats(t.Context(), "ghost"); !errors.Is(err, ErrStackNotFound) {
		t.Fatalf("want ErrStackNotFound, got %v", err)
	}
}

func TestStatsNoRunningContainersReturnsNilNoCall(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	blogDir := registerStack(t, "blog")
	f := &runtime.Fake{Containers: []runtime.Container{
		container("blog-web-1", "kazi-blog", blogDir, "web", "exited", "Exited (0)"),
	}}
	e := testEngine(t, f)
	cs, err := e.Stats(t.Context(), "blog")
	if err != nil || cs != nil {
		t.Fatalf("want nil,nil got %v,%v", cs, err)
	}
	if len(f.StatsCalls) != 0 {
		t.Fatalf("stats should not run with no running containers: %v", f.StatsCalls)
	}
}

func TestStatsStreamEmitsSamples(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	f := &runtime.Fake{StatsOut: `{"ID":"abc","Name":"web","CPUPerc":"1.00%","MemUsage":"10MiB / 20MiB","MemPerc":"50.00%","NetIO":"0B / 0B","BlockIO":"0B / 0B","PIDs":"3"}
{"ID":"abc","Name":"web","CPUPerc":"2.00%","MemUsage":"11MiB / 20MiB","MemPerc":"55.00%","NetIO":"0B / 0B","BlockIO":"0B / 0B","PIDs":"3"}`}
	e := testEngine(t, f)

	ch, err := e.StatsStream(t.Context(), []string{"abc"})
	if err != nil {
		t.Fatal(err)
	}
	var got []StatSample
	for s := range ch {
		got = append(got, s)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 samples, got %d: %+v", len(got), got)
	}
	if got[0].Seq != 0 || got[1].Seq != 1 {
		t.Errorf("seq = %d,%d", got[0].Seq, got[1].Seq)
	}
	if got[0].CPUPercent != 1 || got[1].CPUPercent != 2 {
		t.Errorf("cpu = %v,%v", got[0].CPUPercent, got[1].CPUPercent)
	}
	// Stream mode must be requested.
	if len(f.StatsCalls) != 1 || f.StatsCalls[0][0] != "stream" {
		t.Errorf("want one stream call, got %v", f.StatsCalls)
	}
}

func TestStatsStreamEmptyIDsClosesChannel(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	e := testEngine(t, &runtime.Fake{})
	ch, err := e.StatsStream(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := <-ch; ok {
		t.Fatal("expected an already-closed channel for empty ids")
	}
}

// fakeHost is a scripted hostProvider for HostStats mapping/degradation tests.
type fakeHost struct{ r hostReading }

func (f fakeHost) read(context.Context) hostReading { return f.r }

func TestHostStatsSumsCoresAndMapsBytes(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	e := testEngine(t, &runtime.Fake{})
	e.host = fakeHost{r: hostReading{
		perCPU: []float64{100, 100, 50, 62}, cores: 14,
		memUsed: 12 << 30, memTotal: 36 << 30,
		diskUsed: 884 << 30, diskTotal: 994 << 30,
	}}
	hs, err := e.HostStats(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if hs.CPUPercent != 312 || hs.CPUCores != 14 {
		t.Errorf("cpu = %v%% over %d cores", hs.CPUPercent, hs.CPUCores)
	}
	if hs.MemUsed != 12<<30 || hs.MemTotal != 36<<30 {
		t.Errorf("mem = %d/%d", hs.MemUsed, hs.MemTotal)
	}
	if hs.DiskTotal != 994<<30 {
		t.Errorf("disk total = %d", hs.DiskTotal)
	}
}

func TestHostStatsDegradesDiskButKeepsCPUMem(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	e := testEngine(t, &runtime.Fake{})
	e.host = fakeHost{r: hostReading{
		perCPU: []float64{10}, cores: 1,
		memUsed: 1, memTotal: 2,
		diskErr: errors.New("no such filesystem"),
	}}
	hs, err := e.HostStats(t.Context())
	if err != nil {
		t.Fatalf("a single failed metric must not fail the read: %v", err)
	}
	if hs.CPUPercent != 10 || hs.MemTotal != 2 {
		t.Errorf("cpu/mem should survive: %+v", hs)
	}
	if hs.DiskTotal != 0 {
		t.Errorf("disk should be zeroed, got %d", hs.DiskTotal)
	}
}

func TestHostStatsErrorsOnlyWhenAllMetricsFail(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	e := testEngine(t, &runtime.Fake{})
	e.host = fakeHost{r: hostReading{
		cpuErr:  errors.New("x"),
		memErr:  errors.New("y"),
		diskErr: errors.New("z"),
	}}
	if _, err := e.HostStats(t.Context()); err == nil {
		t.Fatal("want error when every metric fails")
	}
}
