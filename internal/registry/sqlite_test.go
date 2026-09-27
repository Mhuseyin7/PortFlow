package registry

import (
	"errors"
	"testing"
)

func openTemp(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestCreateAndGet(t *testing.T) {
	s := openTemp(t)
	svc, err := s.Create(Service{Hostname: "app.shop.test", TargetHost: "127.0.0.1", TargetPort: 3000, TLS: true, Project: "shop"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if svc.ID == 0 {
		t.Fatal("expected id")
	}
	got, err := s.GetByHostname("APP.shop.test")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Hostname != "app.shop.test" || got.TargetPort != 3000 {
		t.Fatalf("wrong record: %+v", got)
	}
}

func TestDuplicateHostname(t *testing.T) {
	s := openTemp(t)
	if _, err := s.Create(Service{Hostname: "api.shop.test", TargetHost: "127.0.0.1", TargetPort: 8000}); err != nil {
		t.Fatalf("create: %v", err)
	}
	_, err := s.Create(Service{Hostname: "api.shop.test", TargetHost: "127.0.0.1", TargetPort: 8001})
	if !errors.Is(err, ErrDuplicate) {
		t.Fatalf("want ErrDuplicate, got %v", err)
	}
}

func TestDelete(t *testing.T) {
	s := openTemp(t)
	_, _ = s.Create(Service{Hostname: "x.test", TargetHost: "127.0.0.1", TargetPort: 9000})
	if err := s.Delete("x.test"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.GetByHostname("x.test"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if err := s.Delete("x.test"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestStatusUpdate(t *testing.T) {
	s := openTemp(t)
	_, _ = s.Create(Service{Hostname: "h.test", TargetHost: "127.0.0.1", TargetPort: 7000})
	if err := s.UpdateStatus("h.test", StatusUp); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetByHostname("h.test")
	if got.Status != StatusUp {
		t.Fatalf("status not updated: %s", got.Status)
	}
}
