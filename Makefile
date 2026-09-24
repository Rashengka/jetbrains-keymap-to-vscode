BINARY  := jetbrains-keymap-to-vscode
VERSION := $(shell cat internal/release/VERSION)
LDFLAGS := -s -w
TARGETS := darwin/arm64 darwin/amd64 linux/amd64 windows/amd64

.PHONY: build test dist clean

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) .

test:
	go vet ./...
	go test ./...

dist:
	@mkdir -p dist
	@for t in $(TARGETS); do \
		os=$${t%/*}; arch=$${t#*/}; ext=; [ $$os = windows ] && ext=.exe; \
		out=dist/$(BINARY)-$(VERSION)-$$os-$$arch$$ext; echo $$out; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "$(LDFLAGS)" -o $$out . || exit 1; \
	done
	@cd dist && { command -v sha256sum >/dev/null && sha256sum $(BINARY)-* || shasum -a 256 $(BINARY)-*; } > SHA256SUMS && cat SHA256SUMS

clean:
	rm -rf dist $(BINARY)
