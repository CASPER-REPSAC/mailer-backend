# Stage 1: 빌드 단계
FROM golang:1.23 AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o mailer ./cmd/server

FROM alpine:3.17
RUN apk update && apk add --no-cache ca-certificates && update-ca-certificates
WORKDIR /app
COPY --from=builder /app/mailer .
EXPOSE 8000
ENV SESSION_KEY=YOUR_SECRET_SESSION_KEY
CMD ["/app/mailer"]