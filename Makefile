GO ?= go
GOFMT ?= gofmt
VERSION ?= dev
GOOS ?= $(shell $(GO) env GOOS)
GOARCH ?= $(shell $(GO) env GOARCH)
BIN_DIR ?= bin
DIST_DIR ?= dist

ifeq ($(GOOS),windows)
EXE := .exe
else
EXE :=
endif

BIN := $(BIN_DIR)/canopy-doctor$(EXE)
DIST_BIN := $(DIST_DIR)/canopy-doctor_$(VERSION)_$(GOOS)_$(GOARCH)$(EXE)

.PHONY: build check dist fmt race test vet

build:
	mkdir -p "$(BIN_DIR)"
	$(GO) build -buildvcs=false -trimpath -ldflags "-X main.version=$(VERSION)" -o "$(BIN)" ./cmd/canopy-doctor

check:
	$(MAKE) fmt
	$(MAKE) vet
	$(MAKE) test
	$(MAKE) race

dist:
	mkdir -p "$(DIST_DIR)"
	CGO_ENABLED=0 GOOS="$(GOOS)" GOARCH="$(GOARCH)" $(GO) build -buildvcs=false -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o "$(DIST_BIN)" ./cmd/canopy-doctor

fmt:
	@test -z "$$($(GOFMT) -l $$(find cmd internal -type f -name '*.go' -print))" || (echo "run gofmt on listed files"; exit 1)

vet:
	$(GO) vet ./...

test:
	$(GO) test ./...

race:
	$(GO) test -race ./...
