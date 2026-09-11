{ pkgs, piModule }:

# f8-02: декларативная Pi-конфигурация материализуется как симлинки на immutable
# store JSON, не содержит provider-specific discovery и upstream credentials.
pkgs.testers.runNixOSTest {
  name = "pi-config";

  nodes.machine = { ... }: {
    imports = [ piModule ];

    environment.systemPackages = [ pkgs.jq ];

    lattice.pi = {
      enable = true;
      user = "root";
      settings = {
        defaultProvider = "llm-gateway";
        defaultModel = "standard";
        defaultThinkingLevel = "xhigh";
        # pi extension pin: Nix-сборка через pnpm builder (store-path, симлинк-директория).
        extensions = [ pkgs.lattice.pi-mcp-adapter ];
      };
      models.llm-gateway = {
        baseUrl = "http://127.0.0.1:9208/v1";
        api = "openai-completions";
        discoverModels = false;
        models = [
          { id = "standard"; }
          { id = "stupid"; }
        ];
        modelOverrides = {
          standard.compat = { supportsReasoningEffort = false; };
          stupid.compat = { supportsReasoningEffort = false; };
        };
      };
      # f8-03: расширение tool profile проекта.
      tools = [ "nodejs" ];
    };
  };

  testScript = ''
    import json

    machine.start()
    machine.wait_for_unit("multi-user.target")

    agent = "/root/.pi/agent"

    # settings.json и models.json — симлинки на store JSON, а каталог остаётся writable.
    machine.succeed(f"test -L {agent}/settings.json")
    machine.succeed(f"test -L {agent}/models.json")
    machine.succeed(f"test -w {agent}")

    settings = json.loads(machine.succeed(f"cat {agent}/settings.json"))
    assert settings == {
        "defaultProvider": "llm-gateway",
        "defaultModel": "standard",
        "defaultThinkingLevel": "xhigh",
        "extensions": ["${toString pkgs.lattice.pi-mcp-adapter}/extension"],
    }, settings

    models = json.loads(machine.succeed(f"cat {agent}/models.json"))
    provider = models["providers"]["llm-gateway"]

    # Только логические классы, никакого provider-specific discovery.
    assert provider["baseUrl"] == "http://127.0.0.1:9208/v1"
    assert provider["discoverModels"] is False
    assert "apiKey" not in provider, provider
    assert sorted(m["id"] for m in provider["models"]) == ["standard", "stupid"], provider
    assert "apiKey" not in json.dumps(models)

    # Package присутствует и запускается.
    machine.succeed("pi --version")

    # f8-03: tool profile materialised into systemPackages and executable.
    machine.succeed("bash -lc 'command -v git && command -v bash && command -v jq && command -v curl'")
    machine.succeed("bash -lc 'test -x /run/current-system/sw/bin/git'")
    # Project extension available without touching the Pi runtime.
    machine.succeed("bash -lc 'command -v node'")
    # Environment contract file is present and inspectable.
    machine.succeed("test -f /etc/pi.env")
    machine.succeed("grep -q 'GIT_CONFIG_NOSYSTEM=1' /etc/pi.env")
    machine.succeed("grep -q 'LANG=C.UTF-8' /etc/pi.env")

    # Каталог принимает runtime-состояние Pi (sessions/trust) без перезаписи конфига.
    machine.succeed(f"install -d -m 0700 {agent}/sessions")
    machine.succeed(f"touch {agent}/sessions/probe")
    machine.succeed(f"test -f {agent}/sessions/probe")
  '';
}
