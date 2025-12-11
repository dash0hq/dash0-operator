Operator and Collector Memory Usage
===================================

The script `create-many-deployments-and-replicasets.sh` can be used to quickly create a larger number of Kubernetes
objects, and to investigate how memory consumption of the operator manager and the OTel collectors it manages behaves
in relation to that.

The script continuosly creates deployments with multiple revisions (hence more replicasets) in one namespace, each with
replicas=0. This can be used to
a) test the impact of a high numbers of deployments/replicasets on the OTel collector, and
b) test the impact of a high numbers of deployments/replicasets on the operator manager.

The script has a couple of settings (`target_ctx`, `target_namespace`, `max_deployments`, `revisions_per_deployment`,
`max_number_of_replicasets`) that can be tuned to create a specific scenario.

To clean up, simply delete the namespace "operator-k8s-api-load-test" (will take a while).

Note that the speed of creating deployments/replicasets is mainly limited by the duration of the `kubectl apply` call.

If you want to create objects faster, start this script multiple times in parallel, each time with a different prefix,
like so:
```
./create-many-deployments-and-replicasets.sh aaa
./create-many-deployments-and-replicasets.sh bbb
./create-many-deployments-and-replicasets.sh ccc
./create-many-deployments-and-replicasets.sh ddd
```

The limit set via `max_number_of_replicasets` is respected across parallel script invocations.

To inspect the operator manager's heap, you can deploy it with pprof enabled:
```
operator:
  pprofPort: 1777
  collectors:
    # Optional, this is useful to not have the collector pods go OOM due to high number of replicasets, if you want
    # to focus your testing on the operator manager.
    disableReplicasetInformer: true
```

Then run
`kubectl --namespace operator-namespace port-forward $(kubectl get pods --namespace operator-namespace -l app.kubernetes.io/name=dash0-operator -l app.kubernetes.io/component=controller -o jsonpath="{.items[0].metadata.name}") 1777:1777` and use `curl http://localhost:1777/debug/pprof/` to talk
to pprof and `curl http://localhost:1777/debug/pprof/heap > operator_manager.out` to get a heap profile.
Alternatively, attach a debug container as outlined in
<https://github.com/dash0hq/dash0-operator/blob/main/helm-chart/dash0-operator/README.md#create-heap-profiles>

