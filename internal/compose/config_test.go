package compose

import "testing"

// Trimmed real-world shape of `docker compose config --format json`.
const configJSON = `{
  "name": "blog",
  "services": {
    "web": {
      "image": "nginx",
      "ports": [{"mode": "ingress", "target": 80, "published": "8080", "protocol": "tcp"}],
      "networks": {"default": null}
    },
    "db": {
      "image": "postgres:16",
      "expose": ["5432"],
      "networks": {"default": null, "backend": null}
    },
    "worker": {"image": "busybox"}
  }
}`

func TestParseConfig(t *testing.T) {
	svcs, err := ParseConfig([]byte(configJSON))
	if err != nil {
		t.Fatal(err)
	}
	if len(svcs) != 3 {
		t.Fatalf("got %d services", len(svcs))
	}
	// sorted by name: db, web, worker
	db, web, worker := svcs[0], svcs[1], svcs[2]
	if db.Name != "db" || len(db.Ports) != 1 || db.Ports[0] != 5432 {
		t.Errorf("db = %+v", db)
	}
	if len(db.Networks) != 2 || db.Networks[0] != "backend" || db.Networks[1] != "default" {
		t.Errorf("db networks = %v", db.Networks)
	}
	if web.Name != "web" || len(web.Ports) != 1 || web.Ports[0] != 80 {
		t.Errorf("web = %+v", web)
	}
	if web.Published[80] != 8080 {
		t.Errorf("web published = %v", web.Published)
	}
	if worker.Name != "worker" || len(worker.Ports) != 0 || len(worker.Published) != 0 {
		t.Errorf("worker = %+v", worker)
	}
}

func TestParseConfigExposeVariants(t *testing.T) {
	// expose entries may be numbers or strings; duplicate ports (in both
	// ports and expose) must dedupe.
	j := `{"services":{"s":{"ports":[{"target":6379,"published":""}],"expose":[6379,"9121"]}}}`
	svcs, err := ParseConfig([]byte(j))
	if err != nil {
		t.Fatal(err)
	}
	s := svcs[0]
	if len(s.Ports) != 2 || s.Ports[0] != 6379 || s.Ports[1] != 9121 {
		t.Errorf("ports = %v", s.Ports)
	}
	if len(s.Published) != 0 { // empty published string = not host-bound
		t.Errorf("published = %v", s.Published)
	}
}

func TestParseConfigBadJSON(t *testing.T) {
	if _, err := ParseConfig([]byte("not json")); err == nil {
		t.Error("want error for bad JSON")
	}
}
