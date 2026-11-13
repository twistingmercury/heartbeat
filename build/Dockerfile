FROM ghcr.io/twistingmercury/golang-tooling:go1.27.3@sha256:12277dd36b15d77a0a938f5cca30724941c540a28bd95ff25357b3ead8bff4b4

RUN apk add --no-cache bash gcc musl-dev

WORKDIR /workspace

COPY go.mod go.sum ./
RUN go mod download
COPY . .

RUN go vet ./...
RUN test -z "$(goimports -l .)" || (goimports -l . && exit 1)
RUN golangci-lint run
RUN govulncheck ./...
RUN gosec -quiet -exclude-dir=tests ./...
RUN CGO_ENABLED=1 go test -race ./...
RUN go build ./...
