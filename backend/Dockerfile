# Build stage
FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o server ./cmd/server

# Runtime stage
FROM alpine:3.19

# postgresql16-client provides pg_dump used by the in-container backup cron
RUN apk add --no-cache ca-certificates tzdata postgresql16-client

WORKDIR /app

COPY --from=builder /app/server .

EXPOSE 8090

CMD ["./server"]
