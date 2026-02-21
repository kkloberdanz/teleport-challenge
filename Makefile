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

.PHONY: certs
certs:
	mkdir -p certs
	# CA
	openssl ecparam -genkey -name prime256v1 -noout -out certs/ca.key
	openssl req -new -x509 -key certs/ca.key -out certs/ca.crt \
		-days 365000 -subj "/CN=teleworker-ca/O=teleworker"
	# Server
	openssl ecparam -genkey -name prime256v1 -noout -out certs/server.key
	openssl req -new -key certs/server.key \
		-subj "/CN=teleworker" \
		-addext "subjectAltName=DNS:teleworker,DNS:localhost,IP:127.0.0.1" \
	| openssl x509 -req -CA certs/ca.crt -CAkey certs/ca.key \
		-CAcreateserial -out certs/server.crt -days 365000 \
		-copy_extensions copyall
	# Admin client
	openssl ecparam -genkey -name prime256v1 -noout -out certs/admin.key
	openssl req -new -key certs/admin.key -subj "/CN=admin/OU=admin" \
	| openssl x509 -req -CA certs/ca.crt -CAkey certs/ca.key \
		-CAcreateserial -out certs/admin.crt -days 365000
	# Client: alice
	openssl ecparam -genkey -name prime256v1 -noout -out certs/alice.key
	openssl req -new -key certs/alice.key -subj "/CN=alice/OU=client" \
	| openssl x509 -req -CA certs/ca.crt -CAkey certs/ca.key \
		-CAcreateserial -out certs/alice.crt -days 365000
	# Client: bob
	openssl ecparam -genkey -name prime256v1 -noout -out certs/bob.key
	openssl req -new -key certs/bob.key -subj "/CN=bob/OU=client" \
	| openssl x509 -req -CA certs/ca.crt -CAkey certs/ca.key \
		-CAcreateserial -out certs/bob.crt -days 365000

.PHONY: clean
clean:
	rm -rf bin/
	rm -rf certs/
	rm -f proto/teleworker/v1/teleworker_grpc.pb.go
	rm -f proto/teleworker/v1/teleworker.pb.go
