FROM ghcr.io/twistingmercury/golang-tooling:go1.27.2

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
