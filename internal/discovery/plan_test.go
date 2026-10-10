package discovery

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
)

func names(cs []Candidate) []string {
	var out []string
	for _, c := range cs {
		out = append(out, c.Hostname)
	}
	return out
}

func TestPlanGivesEveryServiceAUniqueValidName(t *testing.T) {
	hits := []Hit{
		{Port: 5173, Guess: "app", Process: "node"},
		{Port: 3000, Guess: "app", Process: "node"},
		{Port: 7777, Guess: "svc", Process: "x"},
		{Port: 7778, Guess: "svc", Process: "y"},
	}
	got := names(Plan(hits, nil, "shop", nil))
	want := []string{"app.shop.test", "app-5173.shop.test", "svc.shop.test", "svc-7778.shop.test"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestPlanSkipsOwnAndRegisteredPorts(t *testing.T) {
	hits := []Hit{{Port: 80, Guess: "svc"}, {Port: 9280, Guess: "svc"}, {Port: 3000, Guess: "app"}}
	cs := Plan(hits, nil, "dev", map[int]bool{80: true, 9280: true})
	if len(cs) != 1 || cs[0].Port != 3000 {
		t.Fatalf("PortFlow's own listeners must never be offered for registration: %+v", cs)
	}
}

func TestPlanKeepsOnlyTheContainerForPublishedPorts(t *testing.T) {
	hits := []Hit{{Port: 8080, Guess: "app", Process: "docker-proxy"}, {Port: 3000, Guess: "app", Process: "node"}}
	docker := []DockerHit{{Container: "My_Web_1", Image: "nginx", Port: 8080, TargetPort: 80}}
	cs := Plan(hits, docker, "dev", nil)
	if len(cs) != 2 {
		t.Fatalf("got %+v", cs)
	}
	if cs[0].Source != "process" || cs[0].Port != 3000 || cs[1].Source != "docker" || cs[1].Hostname != "my-web-1.dev.test" {
		t.Fatalf("unexpected plan: %+v", cs)
	}
}

func TestPlanSanitizesProjectAndContainerNames(t *testing.T) {
	cs := Plan(nil, []DockerHit{{Container: "***", Port: 9000}}, "Active Projects", nil)
	if len(cs) != 1 || cs[0].Hostname != "svc.active-projects.test" {
		t.Fatalf("got %+v", cs)
	}
	if cs := Plan([]Hit{{Port: 1, Guess: "app"}}, nil, "", nil); cs[0].Hostname != "app.dev.test" {
		t.Fatalf("empty project must fall back to dev: %+v", cs)
	}
}

func TestPlanCarriesExposure(t *testing.T) {
	cs := Plan([]Hit{{Port: 3000, Guess: "app", Exposed: true}}, nil, "dev", nil)
	if !cs[0].Exposed {
		t.Fatal("a wildcard listener must be flagged")
	}
}

func TestProbeHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404) }))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	if !ProbeHTTP(context.Background(), port) {
		t.Fatal("an HTTP server (even answering 404) must be detected")
	}

	raw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	go func() {
		for {
			c, err := raw.Accept()
			if err != nil {
				return
			}
			c.Close() // a database would not answer HTTP
		}
	}()
	if ProbeHTTP(context.Background(), raw.Addr().(*net.TCPAddr).Port) {
		t.Fatal("a non-HTTP listener must not be treated as a web service")
	}
}
