# Версия для payflow_build_info: тег или хеш коммита, -dirty при незакоммиченных правках.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
IMAGE   ?= payflow
 
run:
	go run ./cmd/payment-api
 
lint:
	golangci-lint run ./...
 
check: fmt lint
	go test ./... -race -count=1
 
fmt:
	golangci-lint fmt ./...
 
docker-build:
	docker build --build-arg VERSION=$(VERSION) -t $(IMAGE):$(VERSION) .
 
# Стенд docker compose: payflow + Prometheus.
up:
	VERSION=$(VERSION) docker compose up --build -d
 
down:
	docker compose down
 
# Проверка prometheus.yml тем же promtool, что лежит в образе Prometheus.
prom-check:
	docker compose run --rm --no-deps --entrypoint promtool prometheus check config /etc/prometheus/prometheus.yml
 