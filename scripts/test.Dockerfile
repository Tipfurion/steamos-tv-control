FROM golang:1.25-bookworm
RUN apt-get update && apt-get install -y --no-install-recommends dbus python3 shellcheck && rm -rf /var/lib/apt/lists/*
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN go test -race ./... && go vet ./...
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /tmp/steamos-tv-control ./cmd/steamos-tv-control
RUN go build -o /usr/local/bin/steamos-tv-control-native ./cmd/steamos-tv-control
COPY install.sh ./
COPY scripts ./scripts
RUN shellcheck install.sh scripts/*.sh
RUN useradd -m -u 1000 tester
USER tester
ENV HOME=/home/tester
CMD ["bash", "-c", "bash scripts/test-installer.sh && python3 scripts/test-tui.py /usr/local/bin/steamos-tv-control-native"]
