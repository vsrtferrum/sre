# syntax=docker/dockerfile:1

FROM golang:1.27.0-alpine AS builder
WORKDIR /src

COPY go.mod ./
RUN go mod download

COPY main.go ./
ARG APP_VERSION=dev
RUN  GOOS=linux go build \
    -ldflags "-X main.version=${APP_VERSION}" \
    -o /out/hw1-app .

FROM alpine:3.21
RUN adduser -D -u 10001 appuser
USER appuser
WORKDIR /app
COPY --from=builder /out/hw1-app /app/hw1-app
EXPOSE 8080
ENTRYPOINT ["/app/hw1-app"]