FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /bin/api ./cmd/api/... \
 && CGO_ENABLED=0 go build -o /bin/healthcheck ./cmd/healthcheck/...

# The media directory is created here, owned by the runtime user, so that a
# fresh named volume mounted on /data/media inherits that owner (the final
# image has no shell to do it).
RUN mkdir -p /data/media && chown 65532:65532 /data/media

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=builder --chown=65532:65532 /data/media /data/media
COPY --from=builder /bin/api /app/api
COPY --from=builder /bin/healthcheck /app/healthcheck
COPY migrations /app/migrations
EXPOSE 8084
ENTRYPOINT ["/app/api"]
