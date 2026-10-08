FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /bin/api ./cmd/api/... \
 && CGO_ENABLED=0 go build -o /bin/healthcheck ./cmd/healthcheck/...

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=builder /bin/api /app/api
COPY --from=builder /bin/healthcheck /app/healthcheck
COPY migrations /app/migrations
EXPOSE 8084
ENTRYPOINT ["/app/api"]
