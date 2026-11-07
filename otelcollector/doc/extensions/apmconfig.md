# `apmconfig` extension

EDOT extension that serves Elastic APM agent central configuration (managed in Kibana) to agents over OpAMP.

Full example: [`apmconfig.example.yaml`](apmconfig.example.yaml)

## Use case on Kubernetes

Applications in the cluster use EDOT SDKs or Elastic APM agents that send to the collector
instead of APM Server. Teams still want to change agent settings (sample rate, log level,
captured headers) from **Kibana › APM › Settings › Agent configuration** without
redeploying pods.

`apmconfig` reads those settings from Elasticsearch and serves them to the agents over OpAMP
on port 4320, so central configuration keeps working with a collector-based setup.

## Where to run it

| Deployment | Use it? | Why |
|---|---|---|
| Gateway (Deployment) | ✅ Yes | One place that talks to Elasticsearch; agents connect to it. |
| Agent (DaemonSet) | Optional | Only if apps are configured to reach the node agent for OpAMP. |

## Configuration

```yaml
extensions:
  apmconfig:
    source:
      elasticsearch:
        endpoint: ${env:ELASTICSEARCH_ENDPOINT}
        auth:
          authenticator: headers_setter/es
    opamp:
      protocols:
        http:
          endpoint: 0.0.0.0:4320
```

Wired into the pipelines:

```yaml
service:
  extensions: [headers_setter/es, apmconfig]
  pipelines:
    traces:
      receivers: [otlp, elasticapmintake]
      processors: [memory_limiter, elasticapm, batch]
      exporters: [elasticsearch]
```

## Kubernetes requirements

- Expose port **4320** on the gateway Service, and point the SDKs' OpAMP endpoint at it.
- Egress to Elasticsearch. The example authenticates with an API key added by the
  [`headers_setter`](headers_setter.md) extension; the key needs read access to the APM
  agent configuration index.

```yaml
apiVersion: v1
kind: Service
metadata: {name: otel-gateway, namespace: otel-gateway}
spec:
  selector: {app: otel-gateway}
  ports:
    - {name: otlp-grpc, port: 4317}
    - {name: otlp-http, port: 4318}
    - {name: opamp,     port: 4320}
    - {name: apm,       port: 8200}   # classic Elastic APM agents (elasticapmintake)
```

## Tips and pitfalls

- Agent settings take effect without pod restarts; that's the main benefit over environment variables in Deployments.
- Protect port 4320 with a NetworkPolicy so only workloads in the cluster can reach it.
- The example also runs `elasticapmintake` on 8200 for classic Elastic APM agents that don't speak OTLP.

## Try it

```sh
./otelcol validate --config doc/extensions/apmconfig.example.yaml
```
