"""Verify the actual public-uplink config with an independent ConfigObj parser."""

import sys
from configobj import ConfigObj

client, custom = (ConfigObj(path, encoding="utf-8", file_error=True, raise_errors=True)
                  for path in sys.argv[1:])
for config in (client, custom):
    assert not config["reticulum"].as_bool("enable_transport")
    for peer in config["interfaces"].values():
        assert peer["type"] == "TCPClientInterface"
        assert "openFirewall" not in peer
        assert "listen_port" not in peer
assert set(client["interfaces"]) == {"Uplink Sydney", "Uplink ReticulumNet"}
for name, host in (("Sydney", "sydney.reticulum.au"), ("ReticulumNet", "node.reticulumnet.nl")):
    peer = client["interfaces"]["Uplink " + name]
    assert peer.as_bool("enabled")
    assert peer["target_host"] == host
    assert peer.as_int("target_port") == 4242
assert set(custom["interfaces"]) == {"Uplink Primary", "Uplink Disabled", "Uplink IPv6"}
assert custom["interfaces"]["Uplink Primary"]["target_host"] == "next.example.net"
assert custom["interfaces"]["Uplink Primary"].as_int("target_port") == 14243
assert not custom["interfaces"]["Uplink Disabled"].as_bool("enabled")
assert custom["interfaces"]["Uplink IPv6"]["target_host"] == "[::1]"
print("Public uplinks, replacement registry, disabled peer and IPv6 config passed.")
