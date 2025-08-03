# jotter

<img src="https://raw.githubusercontent.com/axelknock/jotter/refs/heads/master/web/img/jotter-transparent.svg" alt="jotter the otter" width="33%" align="right">

An online note editor.


```sh
go mod download
```

```sh
go task dev
```

## NixOS

Add this flake to your system configuration:

```nix
{
  inputs.jotter = {
    url = "github:axelknock/jotter";
  };

  outputs = { self, nixpkgs, jotter }: {
    nixosConfigurations.<your host> = nixpkgs.lib.nixosSystem {
      system = "x86_64-linux";
      modules = [
        jotter.nixosModules.default
        {
          services.jotter = {
            enable = true;
            jotDir = "/var/lib/jotter";  # Directory for storing notes
            port = 7086;                 # Port to listen on
            host = "localhost";          # Host to bind to (0.0.0.0 for all interfaces)
            openFirewall = true;         # Open firewall port
          };
        }
      ];
    };
  };
}
```
