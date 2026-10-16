# garage-bootstrap

`garage-bootstrap` creates Garage access keys and buckets and grants their permissions through the Garage Admin API. It reconciles once at startup, then serves `GET /healthz` until shutdown. Restart it to apply configuration changes. It never deletes or revokes Garage state.

## Configuration

Garage-bootstrap can be configured using command-line flags or environment variables. Environment variables use the `GARAGE_BOOTSTRAP_` prefix.

| Flag                | Environment variable               | Default  |
| ------------------- | ---------------------------------- | -------- |
| `--admin-url`       | `GARAGE_BOOTSTRAP_ADMIN_URL`       | Required |
| `--config`          | `GARAGE_BOOTSTRAP_CONFIG`          | Required |
| `--admin-token`     | `GARAGE_BOOTSTRAP_ADMIN_TOKEN`     | Empty    |
| `--request-timeout` | `GARAGE_BOOTSTRAP_REQUEST_TIMEOUT` | `10s`    |
| `--listen-address`  | `GARAGE_BOOTSTRAP_LISTEN_ADDRESS`  | `:8080`  |
| `--dry-run`         | `GARAGE_BOOTSTRAP_DRY_RUN`         | `false`  |

The `--admin-url` points to Garage's **Admin API**, not the S3 endpoint. Pass the Admin token through an environment variable or secret, rather than putting it on a command line.

```sh
garage-bootstrap --admin-url=http://garage-admin:3903 --config=bootstrap.yaml
```

### Bootstrap YAML

The bootstrap configuration has `users` and `policies`, with Garage's native bucket-level `read`, `write`, and `owner` permissions.

```yaml
users:
  - accessKey: GKEXAMPLEBACKUP
    secretKey: REPLACE_WITH_SECRET
    policy: db-backup

policies:
  - name: db-backup
    buckets:
      - name: db-backups
        read: true
        write: true
```

- **Users** import an access key with `accessKey` and `secretKey`, or let Garage generate one by supplying `name` instead. Garage-generated secrets must be retrieved separately from Garage. Imported key IDs must be valid for Garage. `allowCreateBucket` is optional and defaults to `false`.
- **Policies** are reusable: multiple users may reference the same policy. Their bucket entries declare the global aliases to create and their permissions. The permissions `read`, `write`, and `owner` default to `false`. Garage's `write` includes deleting objects. These policies exist only in bootstrap; they are not Garage IAM objects.

Each policy bucket entry creates one grant for every user assigned that policy. Omitting an entry does not revoke existing Garage permissions. Other Garage users, buckets, and permissions are untouched. Unknown YAML fields, duplicate declarations, and invalid bucket names are rejected.

Keep bootstrap YAML private if it contains secrets, and never commit live credentials. Use a dedicated Garage Admin token with the scopes required for your declarations: `ListKeys`, `GetKeyInfo`, `CreateKey`, `ImportKey`, `UpdateKey`, `GetBucketInfo`, `CreateBucket`, and `AllowBucketKey`.

## Deployment

- **Docker Compose:** [`deploy/compose.yaml`](deploy/compose.yaml). See the short setup below.
- **Kubernetes:** [`deploy/kubernetes/README.md`](deploy/kubernetes/README.md). A Deployment reconciles on startup; Stakater Reloader can restart it when its Secret changes.

For Compose, edit the two local files, then start the service:

```sh
cp deploy/bootstrap.example.yaml deploy/bootstrap.yaml
cp deploy/.env.example deploy/.env
chmod 600 deploy/bootstrap.yaml deploy/.env
# Edit the config, Admin token and LOCAL_UID (from `id -u`).
cd deploy
docker compose up -d --build
```

Garage may start after bootstrap. Bootstrap retries transient Admin API failures with capped exponential backoff and exposes health only after reconciliation succeeds. The Compose example uses `http://host.docker.internal:3903`; change the URL in `deploy/compose.yaml` if needed. Health is available at `http://127.0.0.1:8080/healthz`.

## License

[Apache License 2.0](LICENSE).
