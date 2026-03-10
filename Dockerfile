# Stage 1: Build
FROM golang:1.24-alpine AS builder

RUN apk add --no-cache gcc musl-dev

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 go build -o ecorouter ./cmd/server

# Stage 2: Run
FROM alpine:latest

RUN apk add --no-cache ca-certificates

WORKDIR /app
COPY --from=builder /src/ecorouter .
COPY --from=builder /src/web/ ./web/
COPY --from=builder /src/config.yaml .

EXPOSE 8080

CMD ["./ecorouter"]
