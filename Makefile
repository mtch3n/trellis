.PHONY: build build-go test lint ui-audit

# Build both UI and Go binary
build: ui-audit
	cd web && pnpm run build
	CGO_ENABLED=0 go build -o bin/trellis ./cmd/trellis

# Build Go binary only (when Node is unavailable)
build-go:
	CGO_ENABLED=0 go build -o bin/trellis ./cmd/trellis

# UI audit: enforce component consistency
ui-audit:
	cd web && node scripts/ui-audit.js

test:
	go test ./...

lint:
	go vet ./...
