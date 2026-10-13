#!/usr/bin/env bash

set -euo pipefail

cd "$(dirname "$0")"

echo "== 1. create kind cluster =="
kind create cluster --config kind-config.yaml

echo "== 2. build images =="
(cd app && docker build --build-arg APP_VERSION=v1 -t hw1-app:v1 .)
(cd app && docker build --build-arg APP_VERSION=v2 -t hw1-app:v2 .)

echo "== 3. load images into cluster =="
kind load docker-image hw1-app:v1 hw1-app:v2 --name hw1

echo "== 4. apply manifests =="
K="kubectl --context kind-hw1"
$K apply -f kubernetes/
$K rollout status deployment/hw1-app --timeout=120s

echo "== done. Service is available at http://localhost:30080/ =="
$K get pods -o wide
$K get svc hw1-app