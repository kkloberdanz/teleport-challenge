# teleport-challenge

This is my implementation of the Teleport interview challenge.

## Build

### Dependencies

The following includes instructions for installing the dependencies on a Debian 13 Linux system.

#### Protobuf

Protobuf compiler

```sh
sudo apt install -y protobuf-compiler
```

Setup instructions from the [gRPC quickstart guide](https://grpc.io/docs/languages/go/quickstart/#prerequisites).

Go dependencies

```sh
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
```

Update your PATH so that the protoc compiler can find the plugins:

```sh
export PATH="$PATH:$(go env GOPATH)/bin"
```

## Design

Please see the [Design Document](DESIGN.md)
