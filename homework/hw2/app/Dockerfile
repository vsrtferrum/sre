# syntax=docker/dockerfile:1
FROM golang:1.27.0-alpine AS builder
WORKDIR /src
COPY go.mod ./
COPY main.go ./
COPY main_test.go ./
RUN go test ./...
ARG APP_VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags "-X main.version=${APP_VERSION}" -o /out/hw2-app .

FROM alpine:3.21
RUN adduser -D -u 10001 appuser
USER appuser
WORKDIR /app
COPY --from=builder /out/hw2-app /app/hw2-app
EXPOSE 8080 8081
ENTRYPOINT ["/app/hw2-app"]
