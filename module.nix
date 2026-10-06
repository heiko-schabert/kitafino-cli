# NixOS module for kitafino-cli. Import via the flake's nixosModules.default.
self:
{
  config,
  lib,
  pkgs,
  ...
}:
let
  cfg = config.services.kitafino-cli;
in
{
  options.services.kitafino-cli = {
    enable = lib.mkEnableOption "kitafino MCP server over HTTP";
    package = lib.mkOption {
      type = lib.types.package;
      default = self.packages.${pkgs.stdenv.hostPlatform.system}.default;
    };
    listen = lib.mkOption {
      type = lib.types.str;
      default = "127.0.0.1:8080";
      description = "Address for Streamable HTTP.";
    };
    environmentFile = lib.mkOption {
      type = lib.types.path;
      description = "File with KITAFINO_USERNAME and KITAFINO_PASSWORD; kept out of the store.";
    };
    allowWrite = lib.mkEnableOption "order and cancel tools";
    logLevel = lib.mkOption {
      type = lib.types.enum [
        "debug"
        "info"
        "warn"
        "error"
      ];
      default = "info";
    };
  };

  config = lib.mkIf cfg.enable {
    systemd.services.kitafino-cli = {
      wantedBy = [ "multi-user.target" ];
      after = [ "network-online.target" ];
      wants = [ "network-online.target" ];
      environment = {
        KITAFINO_LOG_LEVEL = cfg.logLevel;
      }
      // lib.optionalAttrs cfg.allowWrite { KITAFINO_ALLOW_WRITE = "1"; };
      serviceConfig = {
        ExecStart = "${lib.getExe cfg.package} mcp --http ${cfg.listen}";
        EnvironmentFile = cfg.environmentFile;
        DynamicUser = true;
        Restart = "on-failure";
        NoNewPrivileges = true;
        ProtectSystem = "strict";
        ProtectHome = true;
        PrivateTmp = true;
      };
    };
  };
}
