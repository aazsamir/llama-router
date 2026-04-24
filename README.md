# llama-router

__DISCLAIMER__: This is obviously vibe-coded, I just needed a quick way to keep llama-server ready without hogging GPU/RAM when idle, while using llama.cpp preset config with router support.

A Go wrapper around `llama-server` that proxies requests and kills the process after a configurable idle TTL, freeing memory while keeping models preloaded in the preset.

## Usage

```bash
./llama-router -preset ./preset.ini -ttl 10m
```

Access the server at `http://localhost:11434` — same API as `llama-server`.

## Flags

| Flag | Default | Description |
|---|---|---|
| `-port` | `11434` | Port the wrapper listens on |
| `-ttl` | `180s` | Kill llama-server after this idle duration |
| `-llama-server` | `llama-server` | Path to llama-server binary (searches PATH) |
| `-preset` | `preset.ini` | Path to preset config file |

## Systemd installation

```bash
./install.sh
```

The script build project, then installs the systemd unit, enables it, and starts the service. Logs are available via `journalctl -u llama-router`.

## How it works

1. Wrapper listens on `--port`, llama-server runs internally on `port+1` (bound to `127.0.0.1`).
2. All requests are proxied to the backend.
3. Every incoming request resets the TTL counter.
4. If no requests arrive for the TTL duration, the llama-server process is killed, freeing GPU/RAM memory.
5. On the next request, llama-server is automatically restarted.

