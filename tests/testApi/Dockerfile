FROM ghcr.io/twistingmercury/golang-tooling:go1.27.2@sha256:46ce574e9e3f319ba907e080a87b7e2f32bdbc6f11ab8035c739ab6090703249 AS builder

RUN adduser -D -g '' -u 10001 appuser

RUN apk add --no-cache tzdata
ENV TZ=America/New_York
RUN cp /usr/share/zoneinfo/$TZ /etc/localtime && echo $TZ > /etc/timezone

WORKDIR /build

COPY go.mod go.sum ./
COPY tests/testApi/go.mod tests/testApi/go.sum ./tests/testApi/

WORKDIR /build/tests/testApi

RUN go mod download

WORKDIR /build
COPY . .

WORKDIR /build/tests/testApi

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s" \
    -trimpath \
    -o /build/testapi \
    .

FROM alpine:3.24.2@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6 AS runner

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
# Required for Go's time.LoadLocation() since scratch has no timezone data
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /etc/passwd /etc/passwd
COPY --from=builder /etc/group /etc/group
COPY --from=builder /build/testapi /testapi

USER appuser:appuser

EXPOSE 8080

ENV GIN_MODE=release

ENTRYPOINT ["/testapi"]
