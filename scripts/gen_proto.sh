#!/usr/bin/env sh
# Lints protobuf contracts and regenerates Go code into pkg/pb and pkg/pbpool.
set -e

# Same network settings as the Dockerfile: public proxy and checksum DB only.
export GOPROXY=https://proxy.golang.org,direct
export GOSUMDB=sum.golang.org
export GONOSUMDB=
export GOPRIVATE=

cd "$(dirname "$0")/.."

echo "==> buf lint"
buf lint
echo "==> buf generate"
buf generate
echo "Done."
