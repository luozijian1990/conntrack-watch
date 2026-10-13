CLANG ?= clang
GO ?= go
GOARCH ?= amd64

.PHONY: build bpf test clean
build: bpf
	CGO_ENABLED=0 GOOS=linux GOARCH=$(GOARCH) $(GO) build -trimpath -o build/egress-watch ./cmd/egress-watch
bpf:
	mkdir -p build
	$(CLANG) -target bpfel -mcpu=v1 -O2 -g -Wall -Werror -c bpf/egress.c -o internal/egress/egress.bpf.o
ifeq ($(shell uname -s),Linux)
test: bpf
endif
test:
	$(GO) test -race ./...
clean:
	rm -rf build
	rm -f internal/egress/egress.bpf.o
