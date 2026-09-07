{ pkgs, gatewayModule, gatewayProfile }:

let
  fakeUpstream = pkgs.writeText "fake-llm-upstream.py" ''
    import json
    from http.server import BaseHTTPRequestHandler, HTTPServer

    class Handler(BaseHTTPRequestHandler):
        protocol_version = "HTTP/1.1"

        def log_message(self, _format, *_args):
            pass

        def reply(self, payload, status=200):
            body = json.dumps(payload).encode()
            self.send_response(status)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def do_GET(self):
            if self.path == "/v1/models":
                self.reply({"object": "list", "data": [
                    {"id": "MiniMaxAI/MiniMax-M2.7", "object": "model"},
                    {"id": "deepseek-ai/DeepSeek-V4-Flash-0731", "object": "model"},
                ]})
            else:
                self.reply({"error": "not found"}, 404)

        def do_POST(self):
            size = int(self.headers.get("Content-Length", "0"))
            request = json.loads(self.rfile.read(size))
            if self.headers.get("Authorization") != "Bearer provider-test-key":
                self.reply({"error": "wrong credential"}, 401)
                return
            if request.get("model") not in {
                "MiniMaxAI/MiniMax-M2.7",
                "deepseek-ai/DeepSeek-V4-Flash-0731",
            }:
                self.reply({"error": "wrong model"}, 400)
                return
            self.reply({
                "id": "chat-test",
                "object": "chat.completion",
                "created": 1,
                "model": request["model"],
                "choices": [{"index": 0, "message": {"role": "assistant", "content": "ok"}, "finish_reason": "stop"}],
            })

    HTTPServer(("127.0.0.1", 18080), Handler).serve_forever()
  '';

  clientKey = pkgs.writeText "llm-gateway-test-client-key" "client-test-key\n";
  providerKey = pkgs.writeText "llm-gateway-test-provider-key" "provider-test-key\n";
in
pkgs.testers.runNixOSTest {
  name = "llm-gateway-service";

  nodes.machine = { ... }: {
    imports = [ gatewayModule gatewayProfile ];

    environment.systemPackages = [ pkgs.curl pkgs.jq ];

    lattice.llm-gateway = {
      clientCredentialFile = toString clientKey;
      providers.primary = {
        accessGroup = "test";
        inferenceUrl = "http://127.0.0.1:18080";
        modelsUrl = "http://127.0.0.1:18080/v1/models";
        apiKeyFile = toString providerKey;
        allowPrivateNetwork = true;
      };
      models = [
        { logical = "stupid"; accessGroup = "test"; native = "MiniMaxAI/MiniMax-M2.7"; }
        { logical = "standard"; accessGroup = "test"; native = "deepseek-ai/DeepSeek-V4-Flash-0731"; }
      ];
      routingRules = [
        { model = "stupid"; action = "race"; providers = [ "primary" ]; }
        { model = "stupid"; action = "retry"; attempts = 10; on = [ "429" "5xx" "timeout" "connection_error" ]; }
        { model = "standard"; action = "race"; providers = [ "primary" ]; }
        { model = "standard"; action = "retry"; attempts = 10; on = [ "429" "5xx" "timeout" "connection_error" ]; }
      ];
    };

    systemd.services.fake-llm-upstream = {
      wantedBy = [ "multi-user.target" ];
      serviceConfig = {
        ExecStart = "${pkgs.python3}/bin/python ${fakeUpstream}";
        DynamicUser = true;
      };
    };

    systemd.services.llm-gateway = {
      after = [ "fake-llm-upstream.service" ];
      requires = [ "fake-llm-upstream.service" ];
    };
  };

  testScript = ''
    import json

    machine.start()
    machine.wait_for_unit("fake-llm-upstream.service")
    machine.wait_for_unit("llm-gateway.service")
    machine.wait_for_open_port(9208)

    for logical_model in ["stupid", "standard"]:
        response = json.loads(machine.succeed(
            "curl --fail --silent "
            "-H 'Authorization: Bearer client-test-key' "
            "-H 'Content-Type: application/json' "
            f"-d '{{\"model\":\"{logical_model}\",\"messages\":[{{\"role\":\"user\",\"content\":\"hi\"}}]}}' "
            "http://127.0.0.1:9208/v1/chat/completions"
        ))
        assert response["model"] == logical_model, response
        assert response["choices"][0]["message"]["content"] == "ok", response

    models = json.loads(machine.succeed(
        "curl --fail --silent "
        "-H 'Authorization: Bearer client-test-key' "
        "http://127.0.0.1:9208/v1/models"
    ))
    assert sorted(item["id"] for item in models["data"]) == ["standard", "stupid"], models

    machine.succeed("test $(stat -c %a /run/llm-gateway/config.json) = 600")
    machine.fail("sudo -u nobody cat /run/llm-gateway/config.json")
    machine.succeed("systemctl show -p User --value llm-gateway.service | grep -Fx llm-gateway")
  '';
}
