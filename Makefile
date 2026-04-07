BINARY  = smoko
SRC     = src
VERSION = $(shell cat VERSION)
COMMIT  = $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
LDFLAGS = -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT)

.PHONY: build install cross clean test test-local

build:
	cd $(SRC) && go build -ldflags "$(LDFLAGS)" -o ../$(BINARY).exe ./cmd/smoko

install:
	cd $(SRC) && go install -ldflags "$(LDFLAGS)" ./cmd/smoko

cross:
	cd $(SRC) && GOOS=linux   GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o ../dist/$(BINARY)-linux-amd64   ./cmd/smoko
	cd $(SRC) && GOOS=linux   GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o ../dist/$(BINARY)-linux-arm64   ./cmd/smoko
	cd $(SRC) && GOOS=darwin  GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o ../dist/$(BINARY)-darwin-amd64  ./cmd/smoko
	cd $(SRC) && GOOS=darwin  GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o ../dist/$(BINARY)-darwin-arm64  ./cmd/smoko
	cd $(SRC) && GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o ../dist/$(BINARY)-windows-amd64.exe ./cmd/smoko
	cd $(SRC) && GOOS=windows GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o ../dist/$(BINARY)-windows-arm64.exe ./cmd/smoko

clean:
	rm -f $(BINARY).exe
	rm -rf dist/

test:
	docker run --rm -v "$(CURDIR)/src:/app" -w /app golang:1.22 go test ./... -v -count=1

test-local:
	cd $(SRC) && go test ./... -v -count=1
