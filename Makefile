run:
	go run ./cmd/payment-api

lint:
	golangci-lint run ./...

check: fmt lint
	go test ./... -race -count=1

fmt:
	golangci-lint fmt ./...
