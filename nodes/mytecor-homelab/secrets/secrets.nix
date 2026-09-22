let
  admin = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIP6Gm4DbPs1Ar7/g9IU90YS873SoMYMQhc0xjQFHtJEk mytecor@macbook.local";
  node = "age1dyxfyhf8s5lj9k0pzkkjjte0dcg4yecwglh88kmv2udau0q33v0ssa4pd8";
in
{
  "wifi-ssid.age".publicKeys = [ admin node ];
  "wifi-password.age".publicKeys = [ admin node ];
  "root-password-hash.age".publicKeys = [ admin node ];
  "radicle-private-key.age".publicKeys = [ admin node ];
  # LLM Gateway provider keys
  "llm-provider-gonka-gg-proxy.age".publicKeys = [ admin node ];
  "llm-provider-gonka-gg-openbroker.age".publicKeys = [ admin node ];
  "llm-provider-gonka-api.age".publicKeys = [ admin node ];
  "llm-provider-dahl.age".publicKeys = [ admin node ];
  "llm-provider-dahl-2.age".publicKeys = [ admin node ];
  "llm-provider-hyperfusion.age".publicKeys = [ admin node ];
  "llm-provider-gonkarouter.age".publicKeys = [ admin node ];
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
