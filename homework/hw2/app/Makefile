.PHONY: test build

test:
	go test ./...

build:
	docker build --build-arg APP_VERSION=$$(git rev-parse --short HEAD) -t hw2-app:local .
