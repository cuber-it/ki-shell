VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build install test clean

build:
	go build -ldflags "$(LDFLAGS)" -o kish .

install: build
	cp kish ~/.local/bin/kish
	mkdir -p ~/.local/share/man/man1
	cp kish.1 ~/.local/share/man/man1/kish.1
	@echo "kish $(VERSION) installed to ~/.local/bin/kish"
	@echo "man page installed to ~/.local/share/man/man1/kish.1"

test:
	go test -v ./...

clean:
	rm -f kish

vet:
	go vet ./...
	go mod tidy

release: vet test
	GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o kish-linux-amd64 .
	GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o kish-linux-arm64 .
	GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o kish-darwin-amd64 .
	GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o kish-darwin-arm64 .
	@echo "Binaries built for linux/darwin amd64/arm64"
