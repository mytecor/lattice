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

    # Каталог принимает runtime-состояние Pi (sessions/trust) без перезаписи конфига.
    machine.succeed(f"install -d -m 0700 {agent}/sessions")
    machine.succeed(f"touch {agent}/sessions/probe")
    machine.succeed(f"test -f {agent}/sessions/probe")
  '';
}
