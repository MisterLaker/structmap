FROM golang:1.27-trixie AS devel

ENV APP_PATH=/go/src/devel

RUN set -ex \
    && buildDeps=' \
        gcc \
        musl-dev \
        git \
        curl \
        unzip \
        graphviz \
    ' \
    && apt-get update && apt-get install -y $buildDeps --no-install-recommends \
    && apt-get clean

ENV LINTER_VERSION=v2.15.0

RUN set -ex \
    && curl -sSfL https://golangci-lint.run/install.sh | sh -s -- -b $(go env GOPATH)/bin $LINTER_VERSION

RUN set -ex \
    && go install github.com/go-delve/delve/cmd/dlv@latest

WORKDIR ${APP_PATH}
