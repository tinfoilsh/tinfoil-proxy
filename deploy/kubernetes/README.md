# Deploying tinfoil-proxy on Kubernetes

[tinfoil-proxy](../../README.md) lets an OpenAI-compatible API gateway use Tinfoil: the gateway sends plain HTTP to the proxy, and the proxy verifies a Tinfoil enclave and forwards over a connection pinned to its attested key.

The hop from the gateway to the proxy is plain HTTP with no authentication, so run the proxy as a sidecar in the gateway's own pod. It binds to loopback there: nothing outside the pod can reach it, and prompts and credentials never cross the pod network.

This needs a gateway whose pod spec accepts an extra container. In an AI gateway's chart, look in `values.yaml` for `initContainers`, `extraContainers`, or `sidecars`. A chart that renders `initContainers` verbatim takes the container below unchanged.

Requirements:

- HTTPS egress from the proxy to `atc.tinfoil.sh`, `inference.tinfoil.sh`, and the Tinfoil routers (for example `router-0.tinfoil.sh`).
- A proxy release that serves `/healthz` (any release after `0.3.0-rc.5`).
- If the cluster's egress intercepts TLS, add `--transport ehbp` to the proxy arguments. See the main README for what that mode leaves unprotected.

## 1. Add the container to the gateway pod

Add this to the gateway's pod spec, usually through its chart's `initContainers` value. It requires Kubernetes 1.29 or later.

```yaml
initContainers:
  - name: tinfoil-proxy
    # Pin the digest from the release attestation in production.
    image: ghcr.io/tinfoilsh/tinfoil-proxy:<version>
    # A native sidecar: starts before the gateway and stops after it.
    restartPolicy: Always
    args: ["--bind", "127.0.0.1", "--port", "3301", "--log-format", "json"]
    # The gateway container starts only once the enclave is verified.
    startupProbe:
      exec:
        command: ["wget", "-q", "-O", "/dev/null", "http://127.0.0.1:3301/healthz"]
      periodSeconds: 2
      failureThreshold: 60
    securityContext:
      runAsNonRoot: true
      runAsUser: 65532
      allowPrivilegeEscalation: false
      readOnlyRootFilesystem: true
      capabilities:
        drop: ["ALL"]
    resources:
      requests:
        cpu: 100m
        memory: 64Mi
      limits:
        cpu: 500m
        memory: 256Mi
```

On Kubernetes older than 1.29, list the container under `containers` instead and drop `restartPolicy`. The gateway may then start before the proxy has verified the enclave and must retry.

## 2. Point the gateway at the proxy

Register Tinfoil in the gateway as an OpenAI-compatible provider:

- Base URL: `http://127.0.0.1:3301/v1`
- API key: the Tinfoil API key. The proxy stores no credentials and forwards the key the gateway sends.

## 3. Verify

```sh
# The pod reports every container ready, including tinfoil-proxy.
kubectl get pod <gateway-pod>

# The proxy verified an enclave. Prints the enclave, release, platform, and times as JSON.
kubectl exec <gateway-pod> -c tinfoil-proxy -- \
  wget -q -O - http://127.0.0.1:3301/verification-document

# The proxy reaches the router. Prints the model list.
kubectl exec <gateway-pod> -c tinfoil-proxy -- \
  wget -q -O - http://127.0.0.1:3301/v1/models
```

If the pod stays in `Init`, the proxy has not verified an enclave. Read `kubectl logs <gateway-pod> -c tinfoil-proxy`: a connection error means egress to the Tinfoil domains is blocked, and a certificate error means egress intercepts TLS.

## Notes

- The probe is an `exec` probe because kubelet network probes target the pod IP, which a loopback listener does not answer.
- Each gateway pod runs and verifies its own proxy, so the two scale together.
- To share one prompt-cache scope across pods, set `TINFOIL_USER_CACHE_SECRET` on the container from a Secret. A gateway serving several end users must still send a distinct `user_cache_secret` per user in each request body, otherwise users behind one API key share a cache scope.
