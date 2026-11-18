# tinfoil-proxy chart

Prefer the [sidecar layout](../../kubernetes/README.md), which keeps the gateway-to-proxy hop on loopback. Use this chart when the gateway's pod cannot be changed. It deploys the proxy as its own Deployment behind a ClusterIP Service. Prompts and the provider credential cross the pod network unencrypted, and any pod that can reach the Service can use it.

```sh
helm upgrade --install tinfoil-proxy oci://ghcr.io/tinfoilsh/charts/tinfoil-proxy \
  --version <version> --namespace <namespace>
```

Point the gateway at `http://<release>-tinfoil-proxy.<namespace>:3301/v1`.

Enable `networkPolicy` to restrict who can connect. An enabled policy denies everything that is not listed, and the chart refuses to render one with no egress destinations, so set both sides:

```yaml
networkPolicy:
  enabled: true
  ingress:
    allowedIngressFrom:
      - namespace: gateway
        podSelector:
          app.kubernetes.io/name: llm-gateway
  egress:
    # On Cilium, restrict egress to the trusted Tinfoil domains by name.
    useFQDNEgress: true
    # On other CNIs, list their resolved addresses instead.
    # tinfoilCIDRs: ["203.0.113.10/32"]
```

Verify with `kubectl rollout status deployment/<release>-tinfoil-proxy` and, from a pod allowed by the policy, `wget -q -O - http://<release>-tinfoil-proxy.<namespace>:3301/verification-document`.

See [`values.yaml`](values.yaml) for every setting.
