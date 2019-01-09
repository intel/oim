Dependencies
============

The `vendor` repository is managed with
[dep](https://github.com/golang/dep). A version that includes the git
submodule fix from https://github.com/golang/dep/pull/1909 is required
for vendoring `spdk`, so one has to build `dep` from source.

Runtime dependencies of the production binaries (OIM registry, OIM CSI
driver, OIM controller, oimctl) must pass Intel quality checks before
they can be added. When adding a new runtime component, someone from
Intel must evaluate it and then `runtime-deps.csv` can be updated to
document that it is okay to be used.

All code re-distributed in the `vendor` directory must have a suitable
license. Again, this is something that Intel employees need to
check. The file tracking all components that get distributed is
`vendor-bom.csv`.

That both files are up-to-date is enforced by `make test`.

Code quality
============

Coding style
------------

The normal Go style guide applies. It is enforced by `make test`,
which calls `go fmt` and `gometalinter`

Static code analysis
--------------------

gometalinter needs to be installed separately to run several static
code analysis tools as part of `make test`. Intel also uses commercial
tools.

Input validation
----------------

In all cases, input comes from a trusted source because network
communication is protected by mutual TLS and the `kubectl` binaries
runs with the same privileges as the user invoking it.

Nonetheless, input needs to be validated to catch mistakes:

- detect incorrect parameters for `kubectl`
- ensure that messages passed to gRPC API implementations
  in OIM registry, controller and driver have all required
  fields
- the gRPC implementation rejects incoming messages that are too large
  (https://godoc.org/google.golang.org/grpc#MaxRecvMsgSize) and
  refuses to send messages that are larger
  (https://godoc.org/google.golang.org/grpc#MaxSendMsgSize)
