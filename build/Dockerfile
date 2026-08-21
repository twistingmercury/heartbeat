FROM ghcr.io/twistingmercury/golang-tooling:latest

WORKDIR /workspace

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ENTRYPOINT ["/workspace/build/build.sh"]
