# reqbouncer

A simple helper proxy to forward events from one source to another.

## Usage

First install the reqbouncer binary:

```bash
go install github.com/znowdev/reqbouncer@latest
```

Then you can run the binary with the following command:

```bash
reqbouncer listen --host reqbouncerhost.com:443 --secret-token "my-secret-token" 4000
```

This will start a listener that connects to the `reqbouncerhost.com:443` server and forwards all incoming requests to the `http://localhost:4000` server.


### Configuration

The client can be configured locally by running the following command:

```bash
reqbouncer configure
```

This will create a `~/.reqbouncer/config` file in the user home directory with the following content:

```bash
secret_token="my secret token"
server_host="host:port"
```