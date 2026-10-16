# Kubernetes

This example runs garage-bootstrap as a single-replica Deployment. It reconciles once on startup and serves `/healthz` on port 8080 afterward. If Garage's **Admin API** is not available yet, bootstrap retries with capped exponential backoff.

## Deploy

From the repository root:

```sh
kubectl apply -f deploy/kubernetes/namespace.yaml
cp deploy/kubernetes/secret.example.yaml deploy/kubernetes/secret.yaml
chmod 600 deploy/kubernetes/secret.yaml
# Edit secret.yaml: Admin token, users, policies, buckets.
# Edit deployment.yaml: Garage Admin API URL and container image.
kubectl apply -f deploy/kubernetes/secret.yaml
kubectl apply -f deploy/kubernetes/deployment.yaml
kubectl -n garage rollout status deployment/garage-bootstrap
```

`secret.yaml` is gitignored. Never commit real credentials. Use a dedicated, scoped Garage Admin token, and pin the image to a release or digest in your own deployment.

## Configuration changes

If [Stakater Reloader](https://github.com/stakater/Reloader) is installed, it restarts the Deployment when the `garage-bootstrap` Secret changes:

```sh
kubectl apply -f deploy/kubernetes/secret.yaml
```

Without Reloader, restart manually:

```sh
kubectl -n garage rollout restart deployment/garage-bootstrap
```

`/healthz` signals that startup reconciliation completed; it does not check Garage continuously or detect later drift. The probes access the Pod directly, so no Kubernetes Service or Ingress is required.
