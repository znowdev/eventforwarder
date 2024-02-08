# reqbouncer

A simple helper proxy to forward events from one source to another.

## Usage

First install the reqbouncer binary:

```bash
go install github.com/znowdev/reqbouncer@latest
```

Then you can run the binary with the following command:

```bash
reqbouncer listen --host https://reqbouncer.onrender.com:443 --dest http://localhost:4000 --secret-token "my-secret-token"
```