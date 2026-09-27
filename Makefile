VERSION := 0.1.0
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT)

.PHONY: build test vet fmt clean run dev

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o portflow .

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -s -w .

clean:
	rm -f portflow portflow.exe portflow-*

# Local dev daemon on high ports so no elevation is needed.
dev: build
	./portflow daemon start --http 127.0.0.1:8080 --https 127.0.0.1:8443
