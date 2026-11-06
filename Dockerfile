# The UI is embedded in the binary with go:embed, so the build needs the Go
# sources and nothing else. CGO is off, so the result is a static binary.
FROM golang:1.24-alpine AS build

WORKDIR /src

# Dependencies first: editing the sources then does not refetch them.
COPY go.mod go.sum ./
RUN go mod download

COPY main.go ./
COPY internal/ ./internal/
COPY templates/ ./templates/

RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/lampengeist .

FROM alpine:3.21

# ca-certificates: bridge discovery falls back to the Hue cloud over HTTPS.
# tzdata:          window times are wall-clock times in the container's zone,
#                  so $TZ needs a zone database to mean anything.
RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -u 10001 lampengeist \
    && mkdir -p /data \
    && chown lampengeist:lampengeist /data

COPY --from=build /out/lampengeist /usr/local/bin/lampengeist

USER lampengeist

# config.json is read and written relative to the working directory, so /data
# is the path that has to be persisted.
WORKDIR /data
VOLUME ["/data"]

EXPOSE 64057

ENTRYPOINT ["/usr/local/bin/lampengeist"]
