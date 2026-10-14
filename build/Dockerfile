FROM ghcr.io/twistingmercury/golang-tooling:go1.27.2@sha256:45e8b1fcf8b316516af77c885592e751a7782c0d9fadc6be970e013eebd26267

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
