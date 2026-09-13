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
  "llm-provider-hyperfusion.age".publicKeys = [ admin node ];
  "llm-provider-gonkarouter.age".publicKeys = [ admin node ];
  # f9-04: Attic JWT admin-token secret (EnvironmentFile fragment generated on a
  # linux/builder machine; see modules/attic/README.md "Операторский шаг перед
  # deploy"). Contains ATTIC_SERVER_TOKEN_HS256_SECRET_BASE64="<base64>" or
  # ATTIC_SERVER_TOKEN_RS256_SECRET_BASE64="<base64>". Commit only the .age;
  # never the decrypted value.
  "attic-jwt-secret.age".publicKeys = [ admin node ];
}
