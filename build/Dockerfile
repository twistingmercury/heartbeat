FROM golang:1.24-alpine

RUN apk add --no-cache git bash

WORKDIR /workspace

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ENTRYPOINT ["/workspace/build/build.sh"]
