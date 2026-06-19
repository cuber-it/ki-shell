VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build install test clean

build:
	go build -ldflags "$(LDFLAGS)" -o aish .

install: build
	cp aish ~/.local/bin/aish
	# keep a kish alias for backward compatibility (do not hard-break)
	ln -sf aish ~/.local/bin/kish
	mkdir -p ~/.local/share/man/man1
	cp aish.1 ~/.local/share/man/man1/aish.1
	ln -sf aish.1 ~/.local/share/man/man1/kish.1
	@echo "aish $(VERSION) installed to ~/.local/bin/aish (kish -> aish alias)"
	@echo "man page installed to ~/.local/share/man/man1/aish.1"

test:
	go test -v ./...

clean:
	rm -f aish kish

vet:
	go vet ./...
	go mod tidy

release: vet test
	GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o aish-linux-amd64 .
	GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o aish-linux-arm64 .
	GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o aish-darwin-amd64 .
	GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o aish-darwin-arm64 .
	@echo "Binaries built for linux/darwin amd64/arm64"
