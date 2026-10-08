CLANG ?= clang
GO ?= go
GOARCH ?= amd64

.PHONY: build bpf test clean
build: bpf
	CGO_ENABLED=0 GOOS=linux GOARCH=$(GOARCH) $(GO) build -trimpath -o build/egress-watch ./cmd/egress-watch
bpf:
	mkdir -p build
	$(CLANG) -target bpfel -O2 -g -Wall -Werror -c bpf/egress.c -o build/egress.bpf.o
test:
	$(GO) test -race ./...
clean:
	rm -rf build
