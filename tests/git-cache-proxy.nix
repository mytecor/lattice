{ pkgs, nixpkgs, gitCacheModule, gitCacheProfile }:

# f9-01: Git cache proxy как NixOS-сервис. Поведенческий тест целиком в VM:
# локальный `git http-backend` на 127.0.0.1 служит upstream origin'ом, прокси
# сидит перед ним. Проверяем cold/warm clone (cache hit, тот же commit graph),
# delta fetch новых рефов, disposable-семантику (удаление cache -> обычный
# refetch) и read-only (push через прокси -> 403).
let
  inherit (nixpkgs) lib;

  git = "${lib.getExe' pkgs.git "git"}";
  gitHttpBackend = "${lib.getExe' pkgs.git "git-http-backend"}";

  upstreamPort = 18081;
  proxyPort = 18082;

  # Upstream origin: git smart-HTTP via a tiny CGI server over git-http-backend.
  # Serves bare repos from /srv/git-upstream over http://127.0.0.1:18081.
  upstreamCgi = pkgs.writeText "git-upstream-cgi.py" ''
    import os
    import subprocess
    import sys
    from http.server import BaseHTTPRequestHandler, HTTPServer

    GIT = ${builtins.toJSON git}
    BACKEND = ${builtins.toJSON gitHttpBackend}
    REPOS_ROOT = "/srv/git-upstream"

    class Handler(BaseHTTPRequestHandler):
        protocol_version = "HTTP/1.1"

        def log_message(self, _fmt, *_args):
            pass

        def _serve(self):
            # git-http-backend expects GIT_PROJECT_ROOT, PATH_INFO=/<repo>.git/...
            # and the raw request body; it reads stdin and writes the response.
            try:
                service = parse_qs(self.path.split("?", 1)[1]).get("service", [""])[0] \\
                    if "?" in self.path else ""
            except Exception:
                service = ""
            env = dict(os.environ)
            env.update({
                "GIT_PROJECT_ROOT": REPOS_ROOT,
                "GIT_HTTP_EXPORT_ALL": "1",
                "PATH_INFO": self.path.split("?", 1)[0],
                "REQUEST_METHOD": self.command,
                "QUERY_STRING": self.path.split("?", 1)[1] if "?" in self.path else "",
                "CONTENT_TYPE": self.headers.get("Content-Type", ""),
                "CONTENT_LENGTH": self.headers.get("Content-Length", "0"),
                "REMOTE_ADDR": self.client_address[0],
            })
            body = self.rfile.read(int(env["CONTENT_LENGTH"])) if env["CONTENT_LENGTH"] else b""
            proc = subprocess.run([BACKEND], input=body, env=env,
                                  capture_output=True)
            # git-http-backend emits CGI headers (Status:, Content-Type:) on stdout.
            headers, _, payload = proc.stdout.partition(b"\r\n\r\n")
            if not payload:
                headers, _, payload = proc.stdout.partition(b"\n\n")
            status = 200
            ctype = "application/x-git-upload-pack-advertisement"
            for line in headers.split(b"\r\n"):
                if line.lower().startswith(b"status:"):
                    status = int(line.split(b":", 1)[1].strip().split(b" ", 1)[0])
                elif line.lower().startswith(b"content-type:"):
                    ctype = line.split(b":", 1)[1].strip().decode()
            self.send_response(status)
            self.send_header("Content-Type", ctype)
            self.send_header("Content-Length", str(len(payload)))
            self.end_headers()
            self.wfile.write(payload)

        do_GET = _serve
        do_POST = _serve

    HTTPServer(("127.0.0.1", ${builtins.toString upstreamPort}), Handler).serve_forever()
  '';
in
pkgs.testers.runNixOSTest {
  name = "git-cache-proxy";

  nodes.machine = { ... }: {
    imports = [ gitCacheModule gitCacheProfile ];

    environment.systemPackages = [ pkgs.git pkgs.curl ];

    lattice.git-cache-proxy = {
      enable = true;
      host = "127.0.0.1";
      port = proxyPort;
      upstream = "http://127.0.0.1:${toString upstreamPort}";
      cacheRoot = "/var/cache/git-cache-proxy";
      # Always fetch upstream before serving: the test pushes a fresh commit and
      # must deterministically observe it through the proxy (no TTL coalescing).
      fetchTtlSeconds = 0;
    };

    systemd.tmpfiles.rules = [
      "d /srv/git-upstream 0755 root root - -"
    ];

    systemd.services.git-upstream = {
      description = "Local git smart-HTTP upstream for the cache proxy test";
      wantedBy = [ "multi-user.target" ];
      after = [ "network.target" ];
      serviceConfig = {
        ExecStart = "${pkgs.python3}/bin/python ${upstreamCgi}";
        Restart = "on-failure";
      };
    };

    systemd.services.git-cache-proxy = {
      after = [ "git-upstream.service" ];
      requires = [ "git-upstream.service" ];
    };
  };

  testScript = ''
    machine.start()
    machine.wait_for_unit("git-upstream.service")
    machine.wait_for_open_port(${toString upstreamPort})
    machine.wait_for_unit("git-cache-proxy.service")
    machine.wait_for_open_port(${toString proxyPort})

    # --- service liveness ---
    assert "ok" in machine.succeed(f"curl -fsS http://127.0.0.1:${toString proxyPort}/healthz")
    assert "ok" in machine.succeed(f"curl -fsS http://127.0.0.1:${toString proxyPort}/readyz")

    # --- seed an upstream bare repo with one commit ---
    machine.succeed("""
      set -eu
      export GIT_CONFIG_NOSYSTEM=1 HOME=/root
      git init -q --bare /srv/git-upstream/probe.git
      # Accept anonymous smart-HTTP push from the seed (test-only server) and
      # point HEAD at the branch the seed pushes.
      git --git-dir=/srv/git-upstream/probe.git config http.receivepack true
      git --git-dir=/srv/git-upstream/probe.git symbolic-ref HEAD refs/heads/main
      rm -rf /tmp/seed && git init -q /tmp/seed
      git -C /tmp/seed config user.email test@example.com
      git -C /tmp/seed config user.name test
      echo hello > /tmp/seed/file.txt
      git -C /tmp/seed add file.txt
      git -C /tmp/seed commit -q -m init
      git -C /tmp/seed remote add origin http://127.0.0.1:${toString upstreamPort}/probe.git
      git -C /tmp/seed push -q origin HEAD:refs/heads/main
    """)

    # --- cold clone through the proxy (populates the mirror) ---
    machine.succeed("""
      git -c url."http://127.0.0.1:${toString proxyPort}/".insteadOf="http://127.0.0.1:${toString upstreamPort}/" \\
        clone -q http://127.0.0.1:${toString upstreamPort}/probe.git /tmp/cold
    """)
    expected = machine.succeed("git -C /tmp/cold log -1 --format=%H").strip()

    # Mirror materialised under the cache root.
    machine.succeed("test -d /var/cache/git-cache-proxy/probe.git")

    # --- warm clone served from the mirror, identical commit graph ---
    machine.succeed("""
      git -c url."http://127.0.0.1:${toString proxyPort}/".insteadOf="http://127.0.0.1:${toString upstreamPort}/" \\
        clone -q http://127.0.0.1:${toString upstreamPort}/probe.git /tmp/warm
    """)
    assert machine.succeed("git -C /tmp/warm log -1 --format=%H").strip() == expected

    # --- delta fetch: new upstream commit appears through the proxy ---
    machine.succeed("""
      set -eu
      cd /tmp/seed
      echo second > second.txt && git add second.txt && git commit -q -m second
      git push -q origin HEAD:refs/heads/main
    """)
    machine.succeed(f"cd /tmp/warm && git -c url.\"http://127.0.0.1:${toString proxyPort}/\".insteadOf=\"http://127.0.0.1:${toString upstreamPort}/\" pull -q origin main")
    assert machine.succeed("git -C /tmp/warm log -1 --format=%H").strip() == machine.succeed("git -C /tmp/seed log -1 --format=%H").strip()

    # --- disposable: delete the mirror -> ordinary refetch, same result ---
    machine.succeed("rm -rf /var/cache/git-cache-proxy/probe.git")
    machine.succeed("""
      git -c url."http://127.0.0.1:${toString proxyPort}/".insteadOf="http://127.0.0.1:${toString upstreamPort}/" \\
        clone -q http://127.0.0.1:${toString upstreamPort}/probe.git /tmp/after
    """)
    assert machine.succeed("git -C /tmp/after log -1 --format=%H").strip() == machine.succeed("git -C /tmp/seed log -1 --format=%H").strip()

    # Mirror recreated after refetch.
    machine.succeed("test -d /var/cache/git-cache-proxy/probe.git")

    # --- read-only: push through the proxy is refused ---
    out = machine.fail("""
      cd /tmp/cold && git push http://127.0.0.1:${toString proxyPort}/probe.git HEAD:refs/heads/bad
    """)
    assert "403" in out or "read-only" in out or "receive-pack" in out.lower()

    # --- sandbox: proxy user cannot read upstream secrets (there are none) and
    #     cache dir is 0700 owned by the service user ---
    machine.succeed("test $(stat -c %a /var/cache/git-cache-proxy) = 700")
    machine.succeed("systemctl show -p User --value git-cache-proxy.service | grep -Fx git-cache-proxy")
    machine.succeed("systemctl show -p NoNewPrivileges --value git-cache-proxy.service | grep -Fx yes")
    machine.succeed("systemctl show -p ProtectSystem --value git-cache-proxy.service | grep -Fx strict")
  '';
}
