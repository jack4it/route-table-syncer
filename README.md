# Notes

 For quick local test

```bash
go run . -h
```

Useful make targets

```bash
make build
make push
make deploy
```

## Managed identity

`aadpodidbinding=app-gateway-ingress-ingress-azure`

**Reader**: source route table; usually in the AKS infra resource group

**Network Contributor**: destination route table; usually in the networking resource group

## Service account

The `default` service account, cluster role: `app-gateway-ingress-ingress-azure`
