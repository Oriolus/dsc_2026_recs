# Single image for all three services and the toolbox container.
FROM golang:1.26.11-bookworm

# Public module proxy and checksum DB only: local developer settings
# (corporate proxies, private mirrors) must not affect the build.
ENV GOPROXY=https://proxy.golang.org,direct \
    GOSUMDB=sum.golang.org \
    GONOSUMDB= \
    GOPRIVATE= \
    GOFLAGS=

RUN apt-get update \
    && apt-get install -y --no-install-recommends graphviz jq curl \
    && rm -rf /var/lib/apt/lists/*

# Pinned toolchain, one layer per tool for better cache reuse.
RUN go install github.com/bufbuild/buf/cmd/buf@v1.75.0
RUN go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.12
RUN go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.2
RUN go install github.com/planetscale/vtprotobuf/cmd/protoc-gen-go-vtproto@v0.6.0
RUN go install github.com/bojand/ghz/cmd/ghz@v0.121.0
RUN go install github.com/fullstorydev/grpcurl/cmd/grpcurl@v1.9.4
RUN go install golang.org/x/perf/cmd/benchstat@v0.0.0-20261009192801-be2c69fb417e

WORKDIR /workspace

COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY . .
# Checkouts on Windows may lose the executable bit.
RUN chmod +x scripts/*.sh \
    && go build -o /usr/local/bin/ ./cmd/... \
    && go test -run='^$' ./... > /dev/null
