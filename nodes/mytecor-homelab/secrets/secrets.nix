let
  admin = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIP6Gm4DbPs1Ar7/g9IU90YS873SoMYMQhc0xjQFHtJEk mytecor@macbook.local";
  node = "age1dyxfyhf8s5lj9k0pzkkjjte0dcg4yecwglh88kmv2udau0q33v0ssa4pd8";
in
{
  "wifi-ssid.age".publicKeys = [ admin node ];
  "wifi-password.age".publicKeys = [ admin node ];
  "root-password-hash.age".publicKeys = [ admin node ];
  "radicle-private-key.age".publicKeys = [ admin node ];
  # f15-02: GitHub deploy key (repo-scoped write for mytecor/lattice) for the
  # workspace `publish` push from the node. Created by the operator; see
  # roadmap/f15-node-dev-loop/f15-02.
  "github-lattice-deploy-key.age".publicKeys = [ admin node ];
  # f10-02: r1s cluster join token (r1s1:<...>) for the r1sd allocator, created
  # by the operator 2026-09-27. Public cluster id: fea879387416a033216590028a2ee8776790ced4e2af949fbdcc7e1215d3a5b3
  # (see roadmap/f10-disposable-worker/f10-02-deploy-r1sd.md). Module consumes it
  # once in `worker-runtime` preStart; readable by the r1s service user (0400).
  "r1s-cluster-token.age".publicKeys = [ admin node ];
  # LLM Gateway provider keys
  "llm-provider-gonka-gg-proxy.age".publicKeys = [ admin node ];
  "llm-provider-gonka-gg-openbroker.age".publicKeys = [ admin node ];
  "llm-provider-gonka-api.age".publicKeys = [ admin node ];
  "llm-provider-dahl.age".publicKeys = [ admin node ];
  "llm-provider-dahl-2.age".publicKeys = [ admin node ];
  "llm-provider-hyperfusion.age".publicKeys = [ admin node ];
  "llm-provider-gonkarouter.age".publicKeys = [ admin node ];
  "llm-provider-google-vertex-credentials.age".publicKeys = [ admin node ];
  # Provisioned separately: once this encrypted file exists, the node config
  # automatically adds Google AI Studio to the shared `smart` Gemini pool.
  "llm-provider-google-ai-studio.age".publicKeys = [ admin node ];
  # LLM Gateway client keys (clientKeys): по одному на потребителя. node-pi — Pi
  # на ноде; mac — операторский Mac (mDNS + mesh). Значения созданы оператором
  # (openssl rand, без перевода строки), так что `!cat`-ссылка в Pi и env-путь
  # дают один и тот же точный Bearer.
  "llm-gateway-client-node-pi.age".publicKeys = [ admin node ];
  "llm-gateway-client-mac.age".publicKeys = [ admin node ];
  "grafana-admin-password.age".publicKeys = [ admin node ];
  "grafana-secret-key.age".publicKeys = [ admin node ];
  # F14: Authentik SSO secrets (each .age is a single AUTHENTIK_*=... line).
  "authentik-secret-key.age".publicKeys = [ admin node ];
  "authentik-bootstrap-token.age".publicKeys = [ admin node ];
  "authentik-bootstrap-user.age".publicKeys = [ admin node ];
  "authentik-bootstrap-email.age".publicKeys = [ admin node ];
  "authentik-bootstrap-password.age".publicKeys = [ admin node ];
  # F14: Grafana OAuth2 client secret for authentic Login via OIDC.
  "grafana-oauth-client-secret.age".publicKeys = [ admin node ];
  # f4-05: Yggdrasil node identity (PKCS8 PEM private key — формат PrivateKeyPath).
  # Стабильный адрес ноды в 200::/7 выводится из этого ключа, поэтому ключ живёт в agenix,
  # а не генерируется заново.
  "yggdrasil-keys.age".publicKeys = [ admin node ];
  # f4-05: Cloudflare API token для DNS-01 (валидация ACME-сертификатов *.homelab.myt.su
  # через публичный DNS, т.к. AAAA-записи указывают на yggdrasil-адрес, недостижимый
  # для HTTP-01/TLS-ALPN публичных CA). Создаётся оператором (см. README).
  "caddy-cloudflare-token.age".publicKeys = [ admin node ];
  # f18-08: Jev API keys (опционально). Оператор создаёт .age-файлы при необходимости;
  # пока файла нет, сервисы работают в inspector-режиме (задачи модели требуют ключей).
  "jev-typesafe-api-key.age".publicKeys = [ admin node ];
  "jev-text-model-api-key.age".publicKeys = [ admin node ];
}
