FROM ghcr.io/twistingmercury/golang-tooling:latest

RUN apk add --no-cache bash

WORKDIR /workspace

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ENTRYPOINT ["/workspace/build/build.sh"]
