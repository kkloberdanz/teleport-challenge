export PATH := $(HOME)/go/bin:$(PATH)

.PHONY: build
build:
	go build -o bin/teleworker ./cmd/teleworker
	go build -o bin/telerun ./cmd/telerun

.PHONY: proto
proto:
	protoc \
		--go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		proto/teleworker/v1/teleworker.proto

.PHONY: test
test:
	go test ./...

.PHONY: race
race:
	go test -race ./...

.PHONY: fmt
fmt:
	go fmt ./...

# go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
.PHONY: lint
lint:
	golangci-lint run

.PHONY: clean
clean:
	rm -rf bin/
	rm -f proto/teleworker/v1/teleworker_grpc.pb.go
	rm -f proto/teleworker/v1/teleworker.pb.go
