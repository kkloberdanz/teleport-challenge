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

Notice that some tests have certain dependencies and are skipped if these are
not met. For example, `cgroups` requires running the tests as root. Therefore
the cgroup specific tests will be skipped if the tests are not run as root.

There is a bit of a challenge in testing that the cgroups are applied correctly.
I have a test named `TestCgroupOOMKillsJob` which will verify that a process is
killed if it exceeds the memory limit of 500 MiB. This test uses Python3 to
launch a job that allocates 600 MB (exceeding the limit) and verifies that the
job gets killed. This test is skipped if `python3` is not found in the path.

## Usage

Start the server. Notice, to use `cgroups`, you must run the server as root.
The server will also start as a non-root user, but it not use `cgroups`.

```sh
LOG_LEVEL=info ./bin/teleworker
```

Submit a job:

```sh
LOG_LEVEL=info ./bin/telerun start -- ls -l
```

Get the status of a job:

```sh
./bin/telerun status <job_id>
```

Stop a running job:

```sh
./bin/telerun stop <job_id>
```

By default, `telerun` connects to `127.0.0.1:50051`. Use the `--addr` flag to
specify a different server address:

```sh
./bin/telerun --addr 192.168.1.10:50051 start -- echo hello
```

## Design

Please see the [Design Document](DESIGN.md)
