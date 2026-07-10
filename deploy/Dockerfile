# syntax=docker/dockerfile:1
# Multi-stage build for Postern. Final image is distroless static so the
# binary, plus only what it needs, ships in ~25 MB.
FROM golang:1.22-alpine AS build
WORKDIR /src

# Cache modules separately from sources.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO disabled because modernc.org/sqlite is pure Go. Static linking lets
# us drop into distroless:static.
ENV CGO_ENABLED=0 GOOS=linux
RUN go build -trimpath -ldflags="-s -w" -o /out/postern ./cmd/postern

# Pre-create the data dir owned by distroless's nonroot uid so named volumes
# inherit writable ownership (distroless has no shell to mkdir/chown at run
# time).
RUN mkdir /out/data && chown 65532:65532 /out/data

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
USER nonroot:nonroot
COPY --from=build /out/postern /app/postern
COPY --from=build --chown=65532:65532 /out/data /data

# Document defaults; the operator overrides via -e at run time.
ENV POSTERN_LISTEN_ADDR=:8080 \
    POSTERN_DB_PATH=/data/postern.db
VOLUME ["/data"]
EXPOSE 8080

ENTRYPOINT ["/app/postern"]
