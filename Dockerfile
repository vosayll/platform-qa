FROM golang:1.22-alpine AS builder

WORKDIR /app

RUN apk add --no-cache git make curl

COPY . .
RUN go mod tidy && go mod download
RUN CGO_ENABLED=0 go build -o /app/bin/locali-platform ./cmd/server/main.go

# Run tests during build to ensure 100% integrity
RUN go test -v -count=1 ./tests/...

FROM alpine:3.21

WORKDIR /app

RUN apk add --no-cache curl ca-certificates

COPY --from=builder /app/bin/locali-platform /app/locali-platform
COPY --from=builder /app/web /app/web

EXPOSE 8080

ENV PORT=8080 \
    WEB_DIR=/app/web

CMD ["/app/locali-platform"]
