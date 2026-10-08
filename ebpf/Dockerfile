FROM golang:1.25-bookworm AS toolchain
RUN apt-get update && apt-get install -y --no-install-recommends clang make && rm -rf /var/lib/apt/lists/*

FROM toolchain AS builder
WORKDIR /src
COPY go.mod go.sum ./
ARG GOPROXY=https://proxy.golang.org,direct
RUN go mod download
COPY . .
ARG TARGETARCH
RUN make build GOARCH=${TARGETARCH}

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=builder /src/build/egress-watch /app/egress-watch
COPY --from=builder /src/build/egress.bpf.o /app/egress.bpf.o
USER 0:0
ENTRYPOINT ["/app/egress-watch"]
CMD ["-config", "/etc/egress-watch/config.yaml"]
