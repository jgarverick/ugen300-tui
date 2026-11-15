VERSION ?= dev
LDFLAGS = -s -w -X main.version=$(VERSION)

.PHONY: build test release

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/ugen300-tui .

test:
	go test -timeout 60s ./...
	go vet ./...
	@test -z "$$(gofmt -l main.go internal)"

release:
	@mkdir -p dist
	@set -e; for os in linux darwin windows; do \
		for arch in amd64 arm64; do \
			ext=""; if [ "$$os" = windows ]; then ext=".exe"; fi; \
			echo "Building $$os/$$arch"; \
			CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath \
				-ldflags "$(LDFLAGS)" -o "dist/ugen300-tui-$$os-$$arch$$ext" .; \
		done; \
	done
