# reqbouncer

A simple helper proxy to forward events from one source to another.

## Usage

First install the reqbouncer binary:

```bash
go install github.com/mscno/reqbouncer@latest
```

Then you can run the binary with the following command:

```bash
reqbouncer --host https://reqbouncer.onrender.com:443 -dest http://localhost:8081
```