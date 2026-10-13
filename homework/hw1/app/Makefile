
APP_VERSION ?= v1
IMAGE ?= hw1-app:$(APP_VERSION)

.PHONY: build build-v1 build-v2 load load-v1 load-v2

build:
	docker build --build-arg APP_VERSION=$(APP_VERSION) -t $(IMAGE) .

build-v1:
	docker build --build-arg APP_VERSION=v1 -t hw1-app:v1 .

build-v2:
	docker build --build-arg APP_VERSION=v2 -t hw1-app:v2 .

load:
	kind load docker-image $(IMAGE) --name hw1

load-v1:
	kind load docker-image hw1-app:v1 --name hw1

load-v2:
	kind load docker-image hw1-app:v2 --name hw1