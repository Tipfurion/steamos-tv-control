GO ?= go
VERSION ?= dev

.PHONY: test lint build release test-docker test-systemd
test:
	$(GO) test -race ./...
lint:
	$(GO) vet ./...
	bash -n install.sh
	shellcheck install.sh scripts/*.sh internal/service/network-pre-down.sh
build:
	$(GO) build -o dist/steamos-tv-control ./cmd/steamos-tv-control
release:
	GO="$(GO)" bash scripts/package-release.sh "$(VERSION)"
test-docker:
	bash scripts/test-docker.sh
test-systemd:
	bash scripts/test-systemd.sh
