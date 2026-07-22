# ClusterProfile API Conformance Suite

Conformance tests for the [ClusterProfile API](https://github.com/kubernetes/enhancements/tree/master/keps/sig-multicluster/4322-cluster-inventory)
(KEP-4322), tracked in [issue #60](https://github.com/kubernetes-sigs/cluster-inventory-api/issues/60).

The suite follows the model of the
[MCS-API conformance suite](https://github.com/kubernetes-sigs/mcs-api/tree/master/conformance):
a Ginkgo v2 test binary that runs against any cluster serving the
ClusterProfile API, tags specs as `Required` or `Optional`, links each spec to
the section of KEP-4322 it verifies, and generates both a YAML and an HTML
conformance report.

This directory is a separate Go module (`sigs.k8s.io/cluster-inventory-api/conformance`)
so test-only dependencies stay out of the main API module. It always tests the
API types from the enclosing repository checkout via a `replace` directive.

## Running

From the repository root:

```sh
make test-conformance CONFORMANCE_ARGS="--kubeconfig $HOME/.kube/config"
```

Or directly:

```sh
cd conformance
go test -v ./... -args --kubeconfig $HOME/.kube/config
```

The suite accepts the following flags:

| Flag | Description |
| ---- | ----------- |
| `--kubeconfig` | Absolute path to the kubeconfig of the cluster serving the ClusterProfile API. Standard loading rules (`$KUBECONFIG`, `~/.kube/config`) apply if unset. |
| `--context` | Kubeconfig context to use. Defaults to the current context. |
| `--namespace` | Namespace used for ClusterProfile objects created by the suite. If unset, a temporary namespace is created and removed when the suite completes. |
| `--organization` | Name of the organization responsible for the implementation being tested. |
| `--project` | Name of the implementation project being tested. |
| `--version` | Version of the implementation being tested. |
| `--url` | URL pointing to the implementation project or its documentation. |

Each run writes `report.yaml` and `report.html` into the working directory
(this directory when run via `make` or `go test`); both are gitignored. The
implementation metadata flags are recorded in the reports.

## Running against kind

`make test-conformance-kind` provisions everything and runs the suite in one
step: it downloads `kind` into `bin/`, creates a kind cluster named
`cluster-inventory-conformance` (reusing it if it already exists), installs the
ClusterProfile CRDs, and runs the suite against that cluster. Docker (or
another kind-supported provider) and `kubectl` must be available; everything
else is fetched automatically. Note that creating the cluster switches your
current kubectl context, as usual with kind.

```sh
make test-conformance-kind

# pass extra suite flags:
make test-conformance-kind CONFORMANCE_ARGS="--organization Example --project my-manager"

# pin the node image / Kubernetes version:
make test-conformance-kind KIND_NODE_IMAGE=kindest/node:v1.35.0

# tear the cluster down when done:
make kind-conformance-down
```

## Running against envtest (no cluster required)

`make test-conformance-envtest` runs the suite against a temporary
[envtest](https://book.kubebuilder.io/reference/envtest) control plane with
this repository's CRDs installed — the baseline sanity check for the CRDs
themselves. It downloads the envtest binaries, starts the control plane, runs
the suite, and tears everything down again:

```sh
make test-conformance-envtest
```

The control plane helper can also be run standalone to keep a cluster around
for iterating. From the repository root:

```sh
make envtest
KUBEBUILDER_ASSETS="$(bin/setup-envtest-release-0.23 use 1.35.0 --bin-dir bin -p path)" \
    go run ./hack/conformance-envtest --kubeconfig-out envtest.kubeconfig &
make test-conformance CONFORMANCE_ARGS="--kubeconfig $PWD/envtest.kubeconfig"
kill %1
```

## Importing the suite

The specs and the `TestConformance` entry point live in non-test files so
implementations can embed the suite in their own pipelines, mirroring how
downstreams consume the MCS-API suite:

```go
package conformance_test

import (
	"testing"

	"sigs.k8s.io/cluster-inventory-api/conformance"
)

func TestConformance(t *testing.T) {
	conformance.TestConformance(t)
}
```

The suite registers its flags on the standard `flag` package command line, so
the flags above are available to the importing test binary as well.

## Status

The suite currently covers the core (Required) conformance behaviors:

- the `clusterprofiles` resource is served and namespace scoped
- create, get, list, update and delete of ClusterProfile objects
- rejection of objects without `spec.clusterManager.name`
- immutability of `spec.clusterManager.name` after creation
- status subresource updates (conditions, version, properties,
  accessProviders) and spec/status isolation in both directions
- `metav1.Condition` conventions on status conditions

The extended (Optional) behaviors are covered as well:

- the `x-k8s.io/cluster-manager` label convention and the
  `multicluster.x-k8s.io/clusterset` namespace label convention — both
  observational: they verify ClusterProfile objects managed by a real cluster
  manager and skip when none exist (e.g. on a bare envtest cluster)
- preservation of the KEP-5339 `client.authentication.k8s.io/exec` cluster
  extension on access providers (the `additional-args`/`additional-envs`
  extensions are excluded until the KEP-vs-CRD payload-shape conflict is
  resolved upstream: the KEP specifies bare string arrays, the CRD schema only
  accepts objects)
- coexistence of the deprecated `credentialProviders` field with
  `accessProviders`, stored verbatim without merging
- watch event delivery for create, update and delete
- patch requests through the generated typed clientset

Progress is tracked in
[issue #60](https://github.com/kubernetes-sigs/cluster-inventory-api/issues/60).
