"""Parse generated files with ConfigObj, independently of the Nix renderer."""

import sys

from configobj import ConfigObj


def read(path):
    config = ConfigObj(path, encoding="utf-8", file_error=True, raise_errors=True)
    interfaces = config["interfaces"]
    for interface in interfaces.values():
        assert "openFirewall" not in interface
        assert "extraConfig" not in interface
        assert "null" not in interface.values()
    return config, interfaces


profile, interfaces = read(sys.argv[1])
assert not profile["reticulum"].as_bool("enable_transport")
assert interfaces["Auto Discovery"]["type"] == "AutoInterface"
server = interfaces["TCP Server"]
assert server["type"] == "TCPServerInterface"
assert server.as_bool("enabled")
assert server["listen_ip"] == "0.0.0.0"
assert server.as_int("listen_port") == 4242
assert server.as_int("max_connections") == 64
assert not interfaces["TCP Uplink"].as_bool("enabled")
assert "target_host" not in interfaces["TCP Uplink"]

_, interfaces = read(sys.argv[2])
assert not interfaces["TCP Server"].as_bool("enabled")
client = interfaces["TCP Uplink"]
assert client["type"] == "TCPClientInterface"
assert client.as_bool("enabled")
assert client["target_host"] == "entry.example.net"
assert client.as_int("target_port") == 4242

_, interfaces = read(sys.argv[3])
server = interfaces["TCP Server"]
assert server["listen_ip"] == "127.0.0.1"
assert server.as_int("listen_port") == 14242
assert server.as_int("max_connections") == 8
client = interfaces["TCP Uplink"]
assert client["target_host"] == "[::1]"
assert client.as_int("target_port") == 14243
assert not interfaces["Disabled TCP"].as_bool("enabled")
print("TCP profile, client, overrides and ConfigObj serialization passed.")
