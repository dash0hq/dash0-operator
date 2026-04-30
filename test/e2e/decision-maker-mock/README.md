# decision-maker-mock

Minimal gRPC server that implements `decisionmaker.DecisionMakerService` from
the dash0 monorepo. Used by the operator's e2e tests to keep the Barker
proxy connected to a fake upstream Decision Maker.

The mock does not implement decision-making logic. It accepts streams,
responds enough to keep clients connected, and counts observed RPCs. The
counts are exposed on a separate HTTP debug port so e2e tests can assert
that Barker actually connected.

## Ports

| Port | Protocol | Purpose                                 |
| ---- | -------- | --------------------------------------- |
| 8011 | gRPC     | DecisionMakerService                    |
| 8101 | HTTP     | `/ready`, `/grpc-calls`, `/grpc-calls/reset` |

## Re-vendoring the proto

The proto file at `proto/decisionmaker.proto` is vendored from the dash0
monorepo (`modules/proto/internal/decisionmaker/decisionmaker.proto`) with
the `vtproto` extensions stripped (the mock has no need for the performance
optimizations and dropping them avoids the extra dependency).

To regenerate the bindings after updating the proto:

```sh
make proto-gen-decision-maker-mock
```
