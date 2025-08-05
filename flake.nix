{
  description = "Jotter - A simple note-taking web application";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
        
        jotter = pkgs.buildGoModule {
          pname = "jotter";
          version = "0.1.0";
          
          src = ./.;
          
          vendorHash = "sha256-6zLJqh7dxD6N6RqY36Gb61jLWSuHqMR5uCEENzk1zEU=";

          modVendor = true;
          
          buildFlags = [ "-mod=readonly" ];
          
          doCheck = false;
          
          subPackages = [ "cmd/jotter" ];
          
          meta = with pkgs.lib; {
            description = "A simple note-taking web application";
            homepage = "https://github.com/lucianmot/jotter";
            license = licenses.mit;
            maintainers = [ ];
          };
        };
      in
      {
        packages.default = jotter;
        packages.jotter = jotter;
        
        apps.default = {
          type = "app";
          program = "${jotter}/bin/jotter";
        };
      }
    ) // {
      nixosModules.default = { config, lib, pkgs, ... }:
        with lib;
        let
          cfg = config.services.jotter;
        in
        {
          options.services.jotter = {
            enable = mkEnableOption "Jotter note-taking service";
            
            package = mkOption {
              type = types.package;
              default = self.packages.${pkgs.system}.jotter;
              description = "The Jotter package to use";
            };
            
            jotDir = mkOption {
              type = types.str;
              default = "/var/lib/jotter";
              description = "Directory where jot files are stored";
            };
            
            port = mkOption {
              type = types.port;
              default = 7086;
              description = "Port to listen on";
            };
            
            host = mkOption {
              type = types.str;
              default = "localhost";
              description = "Host to bind to";
            };
            
            user = mkOption {
              type = types.str;
              default = "jotter";
              description = "User to run the service as";
            };
            
            group = mkOption {
              type = types.str;
              default = "jotter";
              description = "Group to run the service as";
            };
            
            openFirewall = mkOption {
              type = types.bool;
              default = false;
              description = "Whether to open the firewall for the specified port";
            };
          };
          
          config = mkIf cfg.enable {
            users.users.${cfg.user} = {
              isSystemUser = true;
              group = cfg.group;
              home = cfg.jotDir;
              createHome = true;
            };
            
            users.groups.${cfg.group} = {};
            
            systemd.services.jotter = {
              description = "Jotter note-taking service";
              wantedBy = [ "multi-user.target" ];
              after = [ "network.target" ];
              
              serviceConfig = {
                Type = "simple";
                User = cfg.user;
                Group = cfg.group;
                Restart = "always";
                RestartSec = "5s";
                
                ExecStart = "${cfg.package}/bin/jotter";
                
                # Security settings
                NoNewPrivileges = true;
                PrivateTmp = true;
                ProtectSystem = "strict";
                ProtectHome = true;
                ReadWritePaths = [ cfg.jotDir ];
                ProtectKernelTunables = true;
                ProtectKernelModules = true;
                ProtectControlGroups = true;
                RestrictRealtime = true;
                RestrictSUIDSGID = true;
                RemoveIPC = true;
                PrivateMounts = true;
              };
              
              environment = {
                JOT_DIR = cfg.jotDir;
                JOT_HOST = cfg.host;
                JOT_PORT = toString cfg.port;
              };
            };
            
            networking.firewall.allowedTCPPorts = mkIf cfg.openFirewall [ cfg.port ];
            
            # Ensure the jot directory exists and has correct permissions
            systemd.tmpfiles.rules = [
              "d ${cfg.jotDir} 0755 ${cfg.user} ${cfg.group} -"
            ];
          };
        };
    };
}
