# wsctl build helpers. `make` builds for this machine; `make release` cross-compiles.
VERSION ?= $(shell date -u +%Y.%m.%d)
LDFLAGS  = -s -w -X main.version=$(VERSION)
BIN      = dist

.PHONY: build test vet fmt install release clean docker

build:            ## build ./wsctl for the current OS/arch
	CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o wsctl .

test:             ## unit tests (fake portal, websocket framing, device-code flow)
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w .

install:          ## go install into $(GOBIN) or ~/go/bin
	CGO_ENABLED=0 go install -trimpath -ldflags="$(LDFLAGS)" .

release: vet test ## cross-compile into dist/
	@mkdir -p $(BIN)
	@for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64; do \
	  os=$${target%/*}; arch=$${target#*/}; ext=""; [ $$os = windows ] && ext=".exe"; \
	  echo "  $$os/$$arch"; \
	  CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags="$(LDFLAGS)" -o $(BIN)/wsctl-$$os-$$arch$$ext . || exit 1; \
	done
	@cd $(BIN) && (sha256sum wsctl-* 2>/dev/null || shasum -a 256 wsctl-*) > SHA256SUMS

docker:           ## container image
	docker build -t workspace/wsctl:$(VERSION) --build-arg VERSION=$(VERSION) .

clean:
	rm -rf wsctl $(BIN)
