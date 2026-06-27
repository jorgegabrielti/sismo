FROM golang:1.26-alpine AS builder

WORKDIR /src

COPY go.mod ./
COPY cmd/ ./cmd/
COPY internal/ ./internal/

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /app/monitor ./cmd/monitor

FROM alpine:latest

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

COPY --from=builder /app/monitor /app/monitor
COPY web/ /app/web/

EXPOSE 8080

ENTRYPOINT ["/app/monitor"]

