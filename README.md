# jotter

A note editor.

## Docker

### Quick script (recommended)

```bash
./run.sh                    # uses ./jots on port 8000
./run.sh -d ~/jots          # custom data dir
./run.sh -d ~/jots -p 3000  # custom dir + port
```

### One‑liner with Docker Compose

```bash
JOTS_DIR=/path/to/jots docker-compose up -d
```

### Manual build & run

```bash
docker build -t jotter .
docker run -d -p 8000:8000 \
  -v /path/to/jots:/app/jots \
  jotter
```

Then open **[http://localhost:8000](http://localhost:8000)**

## Nix

### Run directly from the flake

```bash
nix run github:axelknock/jotter
```

### NixOS service

```nix
services.jotter = {
  enable   = true;
  host     = "0.0.0.0";
  port     = 8000;
  dataDir  = "/srv/jotter";  # optional
};
```

Rebuild:

```bash
sudo nixos-rebuild switch --flake .
```
