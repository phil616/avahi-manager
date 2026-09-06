.PHONY: frontend build dist test vet check detect e2e

frontend:
	cd frontend && npm ci --no-audit --no-fund && npm run build

build: frontend
	mkdir -p bin
	go build -trimpath -o bin/avahi-manager ./cmd/avahi-manager

# Produce the two-file deployment bundle expected by install.sh.
dist: build
	mkdir -p dist
	cp bin/avahi-manager packaging/install.sh dist/
	chmod 0755 dist/avahi-manager dist/install.sh

test: frontend
	go test -race -timeout 90s ./...

vet: frontend
	go vet ./...

check: test vet build

e2e: frontend
	go test -count=1 -tags browser -run TestBrowser -timeout 180s -v ./internal/api

detect:
	go run ./cmd/avahi-manager detect
