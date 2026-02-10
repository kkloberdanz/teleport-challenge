# Tele Worker

Your very own remote worker! Tele Worker is a service that allows you to run commands on a remote Tele Worker Service, where we will run your jobs for you!

Tele Worker is composed of the following components:

- `telerun`: A client side CLI tool for submitting commands to run as jobs.
- `teleworker`: A gRPC based server that receives the jobs from `telerun`, executes them, and streams the outputs back to the client.

The `teleworker` will be split into a `worker/` library directory and a `server/` cmd directory, so that way we can reuse the `worker` code as a library in future projects.

## Scope

I will be targeting the Level 5 challenge, which includes cgroups.

## Design approach

The server (`teleworker`) will implement the following modules:
- `server`: A gRPC server for receiving jobs and allowing client interactions.
- `resources`: Resource control configuration via cgroups.
- `worker`: Responsible for lifetime management of job (i.e. starting, stopping, collecting logs, updating status).
- `job`: The interface for controlling each job. This will provide a standard interface which can be used to extend the worker to support more job types in the future (e.g., docker jobs, etc.). For the scope of this challenge, we will only create a simple implementation of this interface which will only run programs that are currently installed on the host system.

## Interactions

The `telerun` client will have the following commands available:

- `start` to start a job.
- `status` to get the status of a job.
- `logs` to stream the logs from the job.
- `stop` to stop a job.

### Start

- The client will run `telerun start ...` to start a job. See [telerun Usage](#telerun-usage) for examples of using `telerun`.
- This will send an RPC to the server, which is named `teleworker`.
- The server will generate a UUID for the job ID, and return this ID to the client.
- This job ID will be used by the client to query logs, status, or to stop the job.
- A cgroup will be created for this job with the server's hard-coded resource limits applied (see [cgroups](#cgroups)).

Taking a look at the man pages for [cgroups(7)](https://www.man7.org/linux/man-pages/man7/cgroups.7.html), we see example below for how to add a process to a cgroup:
>  Creating cgroups and moving processes
>      A cgroup filesystem initially contains a single root cgroup, '/',
>      which all processes belong to.  A new cgroup is created by
>      creating a directory in the cgroup filesystem:
>
>          mkdir /sys/fs/cgroup/cpu/cg1
>
>      This creates a new empty cgroup.
>
>      A process may be moved to this cgroup by writing its PID into the
>      cgroup's cgroup.procs file:
>
>          echo $$ > /sys/fs/cgroup/cpu/cg1/cgroup.procs

We will want to handle starting out job in a cgroup appropriately. We will want to ensure that the process we are launching will be in the cgroup for that job before it starts up. We also don't want to have our server in the cgroup for the job. Each job will have their own cgroup configuration, and we don't want this configuration applied to the server.

Therefore, we will launch the child process with the `CgroupFD` field of [SysProcAttr.](https://pkg.go.dev/syscall#SysProcAttr) set to the job's cgroup directory. This will ensure that when the job process is launched, the call to [clone(2)](https://www.man7.org/linux/man-pages/man2/clone.2.html) will set `CLONE_INTO_CGROUP` such that the process that gets launched will be in the correct cgroup from the start.


### Status

The job's status can be queried by the client by running the following command:

```sh
telerun status ${JOB_ID}
```

The list of job statuses is as follows:

- **submitted**: The job has been submitted, but has not yet started running.
- **running** - The job is running.
- **success** - The job finished successfully (i.e., with an exit status code of 0).
- **failed** - The job did not finish successfully (i.e., with a non-zero exit status code).
- **killed** - The job was killed before it finished (i.e., with a `StopJob` command).

### Logs

The job's logs can be streamed to the client by running the following command:

```sh
telerun logs ${JOB_ID}
```

- When the worker launches the process, it will combine both stdout and stderr into a single output stream.
- While the job runs, we will concurrently stream its output back to the client.
- For simplicity, we will only buffer the output stream in memory. Preserving output history between server restarts is out of scope. This also means that the service can easily run out of memory (OOM) if the output produces a lot of data.
- Concurrent log streams will be supported.
- Outputs will be treated as raw bytes to ensure that binary data and text data are supported.

Because we have the outputs buffered in memory since job creation, all calls to `telerun logs` will get the entire log contents from the beginning. As a job produces more output, this will get appended to the buffer containing this jobs outputs. Each reader's position within the log buffer will be tracked, and as new outputs are appended to the buffer, we will continue to stream the logs to the reader from that offset position.

We will notify readers of the outputs when more data is available by using [sync.Cond](https://pkg.go.dev/sync#Cond). Readers will call [cond.Wait()](https://pkg.go.dev/sync#Cond.Wait) to await until more data is added to the outputs buffer. The Go routine that appends data to the outputs buffer will call [cond.Broadcast()](https://pkg.go.dev/sync#Cond.Broadcast) to notify the reader that data is available. This way we will not have any busy loops.

### Stop

The client may stop a job before it completes. This is accomplished using the `stop` command like so:

```sh
telerun stop ${JOB_ID}
```

When the client requests for a job to be stopped, `teleworker` will send a `SIGTERM` to the job to attempt to stop it gracefully.
If the job fails to stop in a set period of time (defaults to 10 seconds), `teleworker` will force it to terminate by setting [cgroups.kill](https://lwn.net/Articles/855924/) to `1` for the job.

## telerun Usage

Example usage of how the client program `telerun` will start and interact with jobs. Notice the double dash `--` is used to separate the arguments for `telerun` from the command you will run along with its arguments.

```sh
# Start a job
$ telerun start -- ls -la /tmp
{
  "job_id": "03f8bd5e-fc0a-4039-be56-13f675eb19a0"
}

# Start another job
$ telerun start -- primes.sh
{
  "job_id": "368a7f1f-eb61-43d9-850a-e57b08f84979"
}

# Check job status
$ telerun status 368a7f1f-eb61-43d9-850a-e57b08f84979
{
  "job_id": "368a7f1f-eb61-43d9-850a-e57b08f84979",
  "status": "running"
}

# Stream output (follows until job completes, like tail -f)
$ telerun logs 368a7f1f-eb61-43d9-850a-e57b08f84979
1 is NOT prime
2 is prime
3 is prime
4 is NOT prime
5 is prime
6 is NOT prime
7 is prime
8 is NOT prime
9 is NOT prime
10 is NOT prime

# Stop a job
$ telerun stop 368a7f1f-eb61-43d9-850a-e57b08f84979
```

## cgroups

Resource controls will be implemented via [cgroups(7)](https://www.man7.org/linux/man-pages/man7/cgroups.7.html). The cgroups interface works via editing text files in the `/sys/fs/cgroup` directory. The `teleworker` will implement this as follows:

- Each job will be allocated a cgroup under the `teleworker` parent directory.
- The parent directory, i.e., `/sys/fs/cgroup/teleworker/` will be created when the server starts.
- The job cgroup will be placed inside this directory, like so `/sys/fs/cgroup/teleworker/$JOB_ID`
- The `teleworker` will cleanup the cgroup directory for the job when it terminates.

Resource limits will be hard-coded on the server. Each job will receive the same limits (1 CPU, 500 MiB of RAM, 5 MB/s disk read,
  and 5 MB/s disk write.)

Clients do not configure resource limits; the server applies these values uniformly to every job's cgroup.

The following cgroups controllers will be enabled and enforced for each job:

- **cpu** — The `cpu.max` file will be set to `100000 100000` (100ms quota per 100ms period), which allocates exactly 1 CPU core to the job.
- **memory** — The `memory.max` file will be set to `524288000` (500 MiB in bytes), which caps the job's RAM usage.
- **io** — The `io.max` file will be set to `rbps=5242880 wbps=5242880`, which limits disk read and write throughput to 5 MiB/s each. The block device major/minor number will be discovered at runtime.

We will enable these controllers for child cgroups by writing `+cpu +memory +io` to `cgroup.subtree_control`.

> **Note:** Configuring cgroups requires one of the following:
>
> - root
> - CAP_SYS_ADMIN
> - [cgroup delegation](https://systemd.io/CGROUP_DELEGATION/) through a service such as systemd.
>
> For the simplicity of a short two week challenge, we will run as root, however, for production an alternative is preferable.

## gRPC API

The API to communicate between the client (`telerun`) and the server (`teleworker`) will look like so:

```protobuf
syntax = "proto3";
package teleworker.v1;

service TeleWorker {
  rpc StartJob(StartJobRequest) returns (StartJobResponse);
  rpc GetJobStatus(GetJobStatusRequest) returns (GetJobStatusResponse);
  rpc StreamOutput(StreamOutputRequest) returns (stream StreamOutputResponse);
  rpc StopJob(StopJobRequest) returns (StopJobResponse);
}

message StartJobRequest {
  string command = 1;                  // Command to run.
  repeated string args = 2;            // Arguments to give to the command.
}

message StartJobResponse {
  string job_id = 1;        // Only contains ID for the job that was submitted.
}

// Query the status of the job, used by `telerun status ...`
message GetJobStatusRequest {
  string job_id = 1;
}

message GetJobStatusResponse {
  string job_id = 1;
  JobStatus status = 2;
  int32 exit_code = 3;
}

enum JobStatus {
  JOB_STATUS_UNSPECIFIED = 0;
  JOB_STATUS_SUBMITTED = 1;
  JOB_STATUS_RUNNING = 2;
  JOB_STATUS_SUCCESS = 3;
  JOB_STATUS_FAILED = 4;
  JOB_STATUS_KILLED = 5;
}

// Request the output of stdout and stderr, used by `telerun logs ...`
message StreamOutputRequest {
  string job_id = 1;
}

// Receive the output of stdout and stderr, used by `telerun logs ...`
message StreamOutputResponse {
  bytes data = 1;
}

message StopJobRequest {
  string job_id = 1;
}

message StopJobResponse {}
```

## TLS Setup

TLS stands for **T**ransport **L**ayer **S**ecurity. It is a vital component of network security.

Both `teleworker` and `telerun` will share a Certificate Authority (CA) using a Mutual TLS ([mTLS](https://www.cloudflare.com/learning/access-management/what-is-mutual-tls/)) setup. We will support TLS version 1.3, which is the latest revision as of writing and provides very strong security. With our TLS setup, we will get the benefit of our data being encrypted while it travels over the network, and we can utilize certificates to provide auth. This approach is common in databases, for example, it is the [recommended approach for MongoDB](https://www.mongodb.com/docs/manual/tutorial/configure-x509-client-authentication/).

Go has default configurations for the TLS 1.3 cipher suite. All of which are very secure.
[According to the docs](https://pkg.go.dev/crypto/tls#pkg-constants), Go's TLS implementation supports the following cipher suites:
```go
	// TLS 1.3 cipher suites.
	TLS_AES_128_GCM_SHA256       uint16 = 0x1301
	TLS_AES_256_GCM_SHA384       uint16 = 0x1302
	TLS_CHACHA20_POLY1305_SHA256 uint16 = 0x1303
```

To explain further, we must first note that TLS utilizes both symmetric and asymmetric cryptography. Symmetric cryptography refers to using the same key to encrypt and decrypt data. Asymmetric cryptography refers to using key pairs where one key encrypts and a different key decrypts data. TLS uses asymmetric cryptography to do a handshake and key exchange. TLS will then utilize symmetric cryptography to encrypt the data that it will send over the network. This is done because symmetric algorithms are generally much faster than asymmetric algorithms. Below we will discuss the meaning of these cipher suites.

We see that for each of these TLS cipher suites that use AES (**A**dvanced **E**ncryption **S**tandard) for the symmetric cryptography are properly configured, which I will explain in more detail below.

AES is what is known as a "block cipher". This means that it works on fixed sized "blocks" of bits. In order to encrypt data larger than a block, AES can run in various modes to do so. The mode that is generally recommended by current standards authorities (such as [NIST](https://csrc.nist.gov/pubs/sp/800/38/d/final)) is in the Galois Counter Mode (GCM). GCM avoids the issue of the [ECB Penguin](https://words.filippo.io/the-ecb-penguin/).

We also see that the key sizes are sufficiently large (128 bit and 256 bit). AES with a 128 bit key is generally regarded to be un-crackable by conventional computers.

We also see that the cryptographic hash algorithms are secure, i.e., sha256 and sha384.

As for ChaCha20, it has been designated to be sufficiently secure for [inclusion in OpenSSH](https://www.ietf.org/archive/id/draft-josefsson-ssh-chacha20-poly1305-openssh-01.html). This means that it should be suitable for our use cases.

For the asymmetric cryptography component of the TLS connection, we will generate the certificates using [Elliptic Curve P-256](https://pkg.go.dev/crypto/elliptic#P256) (OpenSSL refers to it this as prime256v1). This is [NIST approved](https://csrc.nist.gov/CSRC/media/Projects/Cryptographic-Algorithm-Validation-Program/documents/dss2/ecdsa2vs.pdf), and elliptic curve is generally preferred over RSA today.

We will generating the certificates, keys, and Certificate Signing Request (CSR) files in a manor similar to this below. Notice we are creating a user named `alice`, and signing Alice's CSR with the CA.

Generate CA key and CA certificate with Elliptic Curve prime256:
```sh
openssl ecparam -genkey -name prime256v1 -noout -out ca.key
openssl req -new -x509 -key ca.key -out ca.crt -days 365 -subj "/CN=TeleWorker CA/O=TeleWorker"
```

Generate the server key, CSR, and certificate. This will be used by the `teleworker` server.
```sh
openssl ecparam -genkey -name prime256v1 -noout -out server.key
openssl req -new -key server.key -out server.csr -subj "/CN=teleworker/O=TeleWorker"
openssl x509 -req -in server.csr -CA ca.crt -CAkey ca.key -out server.crt -days 365
```

Generate the certificate for the user `alice` and sign it with the CA:
```sh
openssl ecparam -genkey -name prime256v1 -noout -out alice.key
openssl req -new -key alice.key -out alice.csr -subj "/CN=alice/OU=client/O=TeleWorker"
openssl x509 -req -in alice.csr -CA ca.crt -CAkey ca.key -out alice.crt -days 365
```

After generating these, they can be verified by OpenSSL like so:
```sh
openssl verify -CAfile ca.crt alice.crt
alice.crt: OK
openssl verify -CAfile ca.crt server.crt
server.crt: OK
```

## Authentication and Authorization

It is important to note the subtle distinction between authentication and authorization. Authentication is how you ensure that "you are who you say you are". More directly, it tells me "who you are". While authorization is a step that comes after authentication which tells me "what you are allowed to do".

### Authentication

Authentication will be controlled via certificates.

This is a common pattern used by databases. See the [MongoDB documentation](https://www.mongodb.com/docs/manual/tutorial/configure-x509-client-authentication/) for configuring x509 authentication. From MongoDB's official documentation, one uses the `CN` field for the username, `OU` for organization unit, and `O` for organization. [Couchbase](https://www.couchbase.com/blog/x-509-certificate-based-authentication/) recommends a very similar approach. To keep this in line with common practices, we will do the same, and borrow the convention from the MongoDB documentation as seen below:
```
subject= CN=myName,OU=myOrgUnit,O=myOrg,L=myLocality,ST=myState,C=myCountry
-----BEGIN CERTIFICATE-----
# ...
-----END CERTIFICATE-----
```

For the purpose of this challenge, there will be no user signup flow. Instead, I will pregenerate the certificates using OpenSSL, and sign them with the CA.

### Authorization

For authorization purposes, each job will have a "job owner", i.e., the user who launched that job. This owner will have exclusive access to the job. This scheme is done in order to keep things simple for the purpose of a two week challenge. A more scalable approach would be to borrow the user/group model from UNIX, where a job can be given permissions both by user and by a group. This however, would complicate the authorization model, so for the scope of this exercise, we will only consider users.

We will also have a special `OU` for `admin` users. These users with an `admin` `OU` will have full access to all jobs on the system.

For each job, we will track who the owner is. To perform authorization, first we will inspect the `CN` field from the certificate to find who is sending the RPC. Next, we will extract that job ID from the RPC. We then will look up the job (using a Map for the initial implementation, but this would be in a database for a production implementation) and if the `CN` is the job owner or the `OU` is `admin`, then we can declare this to be authorized, and we will allow the operation. Otherwise, we will reject it as unauthorized.

## Out of Scope Potential Improvements

These ideas are out of scope for this two week challenge, but could be made in future work to improve this project.

### Security Hardening

This system is not intended for running untrusted code. Additional hardening steps can be taken to better protect our systems from potentially malicious code. It is important to note that security is not an absolute. All thing security related run along on a spectrum of tradeoffs between higher security and convenience. One must decide which tradeoffs are worthwhile.

See an example of a sandboxing program I created for a previous company called [capejail](https://github.com/kkloberdanz/capejail). We can take the concepts implemented in `capejail` and port them to this program to improve security. There are also several off-the-shelf open source sandboxing programs to consider, such as [nsjail](https://github.com/google/nsjail) and [firejail](https://firejail.wordpress.com/)

- **Process ID namespaces:** Launch the program provided in a new process ID (PID) namespace to ensure the untrusted process is unable to see or interact with other processes on the host machine.
- **File system namespaces:** Launch the program provided in a new filesystem namespace to limit the processes's access to the filesystem. This will keep the untrusted code from accessing and manipulating existing files on the system.
- **Chroot:** This is similar the classic FreeBSD approach of isolating code from the host system's filesystem (see [jails](https://docs.freebsd.org/en/books/handbook/jails/)). On Linux, `chroot` is less robust than `jail` on FreeBSD, however a jail-like environment can be emulated with a combination of `chroot`, `namespaces`, and `seccomp`.
- **Unshare network namespace:** This could be an optional flag to launch processes that don't require network access. This would forbid these processes from performing any external networking, which would greatly improve security for programs that don't require networking.
- **Seccomp:** Stands for **Sec**ure **com**puting. It is a way to restrict which syscalls a process is allowed to execute. It can be used to improve security while running untrusted code. The official library [libseccomp](https://github.com/seccomp/libseccomp) offers a convenient way to configure seccomp without needing to get into [Berkeley Packet Filter (BPF)](https://en.wikipedia.org/wiki/Berkeley_Packet_Filter)
- **User namespaces:** Launch the process in a new user namespace so that the user of the process does not have permissions on the host system and is also not able to see other users on the system. An alternative could also be the classic UNIX approach is to use an unprivileged user on the system (See FreeBSD's [nobody](https://docs.freebsd.org/en/books/handbook/basics/#users-system) user account) which can achieve a similar affect.

### Image format

Currently, we are only running programs that are already installed on the host machine. We could expand this to take an OCI compatible container image format so that we will have a degree of compatibility with standard tools such as Docker or Podman.

### In memory output buffering

To keep the coding challenge simple, outputs are kept in memory. For a production setup, we would want to write outputs to disk, both for crash recovery, and to ensure that the worker does not run out of memory.

### No persistence

Job state is in memory only. A server restart loses all job history. Production would persist state to disk (ideally using a database).

### Additional cgroup controls.

It may be worthwhile to explore even more cgroup controls. For example, one may consider limiting the number of processes for a job to avoid something like a fork bomb.
