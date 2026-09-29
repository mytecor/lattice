{ lib, pkgs, ... }:

let
  inherit (lib) mkOption types;
in
{
  options.lattice.llm-gateway = {
    enable = mkOption {
      type = types.bool;
      default = false;
      description = "Enable the Lattice OpenAI-compatible LLM gateway.";
    };

    package = mkOption {
      type = types.package;
      default = pkgs.lattice.llm-gateway;
      defaultText = lib.literalExpression "pkgs.lattice.llm-gateway";
      description = "Package providing the gateway binary.";
    };

    user = mkOption {
      type = types.str;
      default = "llm-gateway";
      description = "Unprivileged user that runs the gateway.";
    };

    group = mkOption {
      type = types.str;
      default = "llm-gateway";
      description = "Group that runs the gateway.";
    };

    runtimeDirectory = mkOption {
      type = types.strMatching "[A-Za-z0-9][A-Za-z0-9_.-]*";
      default = "llm-gateway";
      description = "systemd RuntimeDirectory name below /run.";
    };

    settings = mkOption {
      type = types.submodule {
        freeformType = types.attrsOf types.anything;
      };
      default = { };
      description = ''
        Public gateway configuration written verbatim as JSON. Keys use the
        gateway's native snake_case names; the NixOS module neither rewrites
        fields nor duplicates the Go configuration schema.
      '';
    };

    credentials = mkOption {
      type = types.attrsOf types.str;
      default = { };
      description = ''
        Environment variable name to runtime secret-file path mapping. Public
        settings refer to these values as env.NAME; secret contents are loaded
        by systemd and never enter the Nix store. Build-time validation requires
        the declared names and public env.NAME references to match exactly.
      '';
    };

    publicConfigFile = mkOption {
      type = types.path;
      readOnly = true;
      description = "Generated and gateway-validated non-secret JSON configuration.";
    };
  };
}
