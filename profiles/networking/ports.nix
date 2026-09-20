{
  llm-gateway = 9208;
  pi-acp = 55514;
  radicle-node = 8776;
  radicle-httpd = 8080;
  rns-auto-discovery = 29716;
  rns-auto-data = 42671;
  rns-tcp = 4242;
  git-cache-proxy = 9211;
  verdaccio = 9212;
  # F12 observability (loopback-only, non-public by design).
  prometheus = 9213;
  loki = 9214;
  grafana = 9215;
  alloy = 9216;
  # F14: central SSO (Authentik). Loopback-only; the `auth` Caddy site is the ingress.
  authentik = 9220;
}
