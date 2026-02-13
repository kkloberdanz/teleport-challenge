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

### Compile

Generate protobuf code:

```sh
make proto
```

Build the `teleworker` and `telerun` binaries:

```sh
make build
```

### Test

Run tests (includes goroutine leak detection via [goleak](https://github.com/uber-go/goleak)):

```sh
make test
```

Run tests with the race detector:

```sh
make race
```

## Usage

Start the server:

```sh
LOG_LEVEL=info ./bin/teleworker
```

Submit a job:

```sh
LOG_LEVEL=info ./bin/telerun start -- ls -l
```

## Design

Please see the [Design Document](DESIGN.md)
