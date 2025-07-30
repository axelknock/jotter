{
  description = "jotter – collaborative editor";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs =
    {
      self,
      nixpkgs,
      flake-utils,
    }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = import nixpkgs { inherit system; };
        py = pkgs.python313;

        ds = py.pkgs.buildPythonPackage {
          pname = "datastar-py";
          version = "0.6.3";
          format = "wheel";
          src = pkgs.fetchPypi {
            pname = "datastar_py";
            version = "0.6.3";
            format = "wheel";
            python = "py3";
            abi = "none";
            platform = "any";
            sha256 = "sha256-bELO1SKf5+n7VP1AXo+9UXSpyZKU2W5g3M03eCnqMKM=";
          };
          doCheck = false;
        };

        pyEnv = py.withPackages (ps: [
          ps.sanic
          ds
        ]);

        jotter = pkgs.stdenv.mkDerivation {
          pname = "jotter";
          version = "0.1.0";
          src = ./.;
          buildInputs = [ pyEnv ];
          installPhase = ''
            mkdir -p $out/{bin,share/jotter}
            cp main.py $out/share/jotter/
            cat > $out/bin/jotter <<EOF
            #!${pkgs.bash}/bin/bash
            exec ${pyEnv}/bin/python $out/share/jotter/main.py "\$@"
            EOF
            chmod +x $out/bin/jotter
          '';
          meta.license = pkgs.lib.licenses.mit;
        };
      in
      {
        packages.default = jotter;
      }
    )
    // {
      nixosModules.default =
        {
          config,
          pkgs,
          lib,
          ...
        }:
        with lib;
        let
          cfg = config.services.jotter;
          pkg = self.packages.${pkgs.system}.default;
        in
        {
          options.services.jotter = {
            enable = mkEnableOption "Run the Jotter service";
            host = mkOption {
              type = types.str;
              default = "0.0.0.0";
            };
            port = mkOption {
              type = types.port;
              default = 7708;
            };
            openFirewall = mkOption {
              type = types.bool;
              default = false;
            };
          };

          config = mkIf cfg.enable {
            systemd.services.jotter = {
              description = "Jotter";
              wantedBy = [ "multi-user.target" ];
              serviceConfig.ExecStart = "${pkg}/bin/jotter";
              environment = {
                JOT_HOST = cfg.host;
                JOT_PORT = toString cfg.port;
              };
            };

            networking.firewall.allowedTCPPorts = mkIf cfg.openFirewall [ cfg.port ];
          };
        };
    };
}
