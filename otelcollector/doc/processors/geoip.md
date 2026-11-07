# `geoip` processor

Looks up IP addresses in a MaxMind database and adds geo attributes (country, city, location).

Full example: [`geoip.example.yaml`](geoip.example.yaml)

## Use case on Kubernetes

Front-end services (web, mobile backends) record the end user's IP in `client.address`.
Adding geography lets Kibana show maps and per-country error rates. On Kubernetes, the
important part is getting the **real client IP**: by default, traffic through a
`LoadBalancer` Service or Ingress is source-NATed and the app only sees a node or proxy IP.

## Where to run it

| Deployment | Use it? | Why |
|---|---|---|
| Gateway (Deployment) | ✅ Yes | One copy of the database, mounted once. |
| Agent (DaemonSet) | Optional | Works, but every node then needs the database file. |

## Configuration

```yaml
processors:
  geoip:
    context: record          # read the IP from span/log attributes (not the resource)
    attributes: [client.address, source.address]
    providers:
      maxmind:
        database_path: ${env:GEOIP_DATABASE}
```

Wired into the pipelines:

```yaml
service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [memory_limiter, geoip, batch]
      exporters: [elasticsearch]
    logs:
      receivers: [otlp]
      processors: [memory_limiter, geoip, batch]
      exporters: [elasticsearch]
```

## Kubernetes requirements

- **Database:** download `GeoLite2-City.mmdb` (free MaxMind account) and provide it as a
  volume. A ConfigMap can't hold it (1 MiB limit), so use an init container, a PVC or a
  CSI volume.
- **Real client IPs:** set `externalTrafficPolicy: Local` on the `LoadBalancer` Service, or
  have the Ingress pass `X-Forwarded-For` and have the app record it.

One way to provide the database:

```yaml
# Gateway Deployment (excerpt): init container downloads the DB into an emptyDir
initContainers:
  - name: geoip-db
    image: ghcr.io/maxmind/geoipupdate:v7.1.1
    env:
      - {name: GEOIPUPDATE_EDITION_IDS, value: GeoLite2-City}
      - {name: GEOIPUPDATE_DB_DIR, value: /geoip}
      - name: GEOIPUPDATE_ACCOUNT_ID
        valueFrom: {secretKeyRef: {name: maxmind, key: account-id}}
      - name: GEOIPUPDATE_LICENSE_KEY
        valueFrom: {secretKeyRef: {name: maxmind, key: license-key}}
    volumeMounts: [{name: geoip, mountPath: /geoip}]
containers:
  - name: otel-gateway
    env:
      - {name: GEOIP_DATABASE, value: /etc/otel/geoip/GeoLite2-City.mmdb}
    volumeMounts: [{name: geoip, mountPath: /etc/otel/geoip, readOnly: true}]
volumes:
  - {name: geoip, emptyDir: {}}
```

## Tips and pitfalls

- The collector fails to start if the database file is missing.
- `context: record` reads IPs from span/log attributes; `context: resource` reads resource attributes.
- Private IPs (10.x, 172.16-31.x, 192.168.x) have no geo data. Seeing only those means you're getting pod or node IPs, not client IPs.
- Refresh the database regularly (MaxMind updates it weekly), for example with a CronJob or by restarting with the init container.

## Try it

```sh
./otelcol validate --config doc/processors/geoip.example.yaml
```
