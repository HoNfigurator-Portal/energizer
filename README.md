# Energizer

Manages Heroes of Newerth (HoN) game server instances for [Project Kongor](https://kongor.net). Includes a built-in web dashboard for monitoring and control.

---

## Requirements

- **HoN Game Server files** (provided by Project Kongor)
- **Project KONGOR hosting account** (whitelisted login)
- **OS**: Windows 10/11 or Linux (x64)
- **Ports**: Game ports + API port must be open (firewall / port forwarding)

---

## Installation

### Option 1: Download the pre-built binary

Download `energizer.exe` (Windows) or `energizer` (Linux) and place it in a folder of your choice.

### Option 2: Build from source

Requires [Go 1.22+](https://go.dev/dl/) and [Node.js 18+](https://nodejs.org/) (for the dashboard).

```bash
# Clone the repository
git clone https://github.com/HoNfigurator-Portal/energizer.git
cd energizer

# Build the dashboard
cd dashboard
npm install
npm run build
cd ..

# Build for Windows
go build -o energizer.exe ./cmd/energizer

# Build for Linux
GOOS=linux GOARCH=amd64 go build -o energizer ./cmd/energizer
```

> On Windows PowerShell, use this for cross-compilation:
> ```powershell
> $env:GOOS="linux"; $env:GOARCH="amd64"; go build -o energizer ./cmd/energizer
> ```

---

## Folder Structure

Place the following files alongside the Energizer binary:

```
energizer/
├── energizer.exe            # Main binary (or 'energizer' on Linux)
├── config/
│   └── config.json          # Configuration (auto-created on first run)
├── dashboard/
│   └── dist/                # Dashboard web files (built from source)
│       ├── bg/bg.png        # Background image (optional)
│       ├── icon/icon.png    # Favicon (optional)
│       └── logo/logo.png    # Logo image (optional)
└── logs/                    # Log files (auto-created)
```

---

## Configuration

On first run, Energizer creates `config/config.json` with default values. Edit this file to match your setup.

### HoN Server Settings (`hon_data`)

| Key | Description | Example |
|-----|-------------|---------|
| `hon_install_directory` | Path to HoN game client folder | `C:\HoN` or `/opt/hon` |
| `hon_home_directory` | Path to HoN home directory (usually same as install) | `C:\HoN` |
| `hon_artefacts_directory` | Path to HoN artefacts (usually same as install) | `C:\HoN` |
| `hon_executable_name` | Game server executable name | `hon_x64.exe` (Windows) / `hon_x64` (Linux) |
| `svr_login` | Project Kongor hosting account username | `MyHost` |
| `svr_password` | Project Kongor hosting account password | `MyPassword123` |
| `svr_name` | Server display name prefix (shown in game lobby) | `SEA` |
| `svr_location` | Server region code | `SEA`, `US`, `EU`, etc. |
| `svr_ip` | Server public IP (leave empty for auto-detect) | `1.2.3.4` |
| `svr_total` | Number of game server instances to run | `5` |
| `svr_total_per_core` | Max instances per CPU core | `1` |
| `svr_starting_gamePort` | First game port (subsequent servers increment by 1) | `11235` |
| `svr_starting_voicePort` | First voice port | `11335` |
| `svr_managerPort` | TCP listener port for game server communication | `1134` |
| `svr_api_port` | REST API + Dashboard port | `5000` |
| `svr_masterServer` | Master server address (host:port) | `api.kongor.net` |
| `svr_chatAddress` | Chat server address | `chat.kongor.net` |
| `svr_chatPort` | Chat server port | `11032` |
| `man_enableProxy` | Enable built-in DDoS protection proxy | `true` / `false` |
| `svr_allow_bot_matches` | Allow bot match creation | `true` / `false` |
| `svr_override_affinity` | Override CPU affinity post-launch (Windows) | `true` / `false` |
| `svr_beta_mode` | Enable beta mode | `true` / `false` |
| `svr_noConsole` | Hide game server console windows | `true` / `false` |
| `svr_max_idle_time` | Max idle time in seconds before auto-restart | `60` |
| `svr_version` | Force specific game version (leave empty for auto) | `""` |

### Application Settings (`application_data`)

| Section | Key | Description | Default |
|---------|-----|-------------|---------|
| **Logging** | `logging.level` | Log level: `debug`, `info`, `warn`, `error` | `info` |
| | `logging.directory` | Log file directory | `logs` |
| | `logging.max_size_mb` | Max log file size before rotation | `10` |
| | `logging.max_backups` | Number of old log files to keep | `5` |
| **Replay Cleaner** | `replay_cleaner.enabled` | Auto-delete old replays | `true` |
| | `replay_cleaner.retention_days` | Keep replays for N days | `7` |
| | `replay_cleaner.cleanup_time` | Daily cleanup time (24h format) | `04:00` |
| **Discord** | `discord.webhook_url` | Discord webhook URL for notifications | `""` |
| | `discord.notify_on_lag` | Notify on server lag | `true` |
| | `discord.notify_on_crash` | Notify on server crash | `true` |
| | `discord.notify_on_disk` | Notify on low disk space | `true` |
| **Security** | `security.auth_disabled` | Disable API authentication (local use) | `true` |
| | `security.rate_limit_rps` | API rate limit (requests/second) | `100` |
| | `security.tls_enabled` | Enable HTTPS for API | `false` |

### Example config.json

```json
{
  "hon_data": {
    "hon_install_directory": "C:\\HoN",
    "hon_home_directory": "C:\\HoN",
    "hon_artefacts_directory": "C:\\HoN",
    "hon_executable_name": "hon_x64.exe",
    "svr_login": "MyHost",
    "svr_password": "MyPassword",
    "svr_name": "SEA",
    "svr_location": "SEA",
    "svr_ip": "",
    "svr_total": 5,
    "svr_total_per_core": 1,
    "svr_starting_gamePort": 11235,
    "svr_starting_voicePort": 11335,
    "svr_managerPort": 1134,
    "svr_api_port": 5000,
    "svr_masterServer": "api.kongor.net",
    "svr_chatAddress": "chat.kongor.net",
    "svr_chatPort": 11032,
    "man_enableProxy": false,
    "man_use_cowmaster": false,
    "svr_beta_mode": false,
    "svr_noConsole": false,
    "svr_override_affinity": true,
    "svr_allow_bot_matches": true,
    "svr_max_idle_time": 60,
    "svr_version": ""
  },
  "application_data": {
    "logging": {
      "level": "info",
      "directory": "logs",
      "max_size_mb": 10,
      "max_backups": 5
    },
    "replay_cleaner": {
      "enabled": true,
      "cleanup_time": "04:00",
      "retention_days": 7,
      "tmp_retention_days": 1
    },
    "discord": {
      "webhook_url": "",
      "notify_on_lag": true,
      "notify_on_crash": true,
      "notify_on_disk": true
    },
    "security": {
      "auth_disabled": true,
      "rate_limit_rps": 100,
      "tls_enabled": false
    }
  }
}
```

---

## Running

### Windows

1. Double-click `energizer.exe` (will auto-request Administrator privileges)
2. On first run, edit `config/config.json` with your settings, then restart
3. Game servers will start automatically

### Linux

```bash
chmod +x energizer
sudo ./energizer
```

> **Note:** `sudo` is recommended for process management and port binding.

### What happens on startup

1. Energizer loads `config/config.json`
2. Cleans up leftover game server processes from previous runs
3. Auto-detects server IP if `svr_ip` is empty
4. Connects to the master server and chat server
5. Launches all game server instances
6. Starts the REST API + Dashboard on the configured port
7. Begins health monitoring and scheduled tasks

### Verifying servers are online

- Check the dashboard at `http://localhost:5000` (or your configured `svr_api_port`)
- In the game client, your servers should appear in the server list
- Console windows (Windows) will show heartbeat messages: `Sent heartbeat`

---

## Dashboard

Open your browser and go to:

```
http://<server-ip>:<svr_api_port>
```

For example: `http://localhost:5000`

### Pages

| Page | Description |
|------|-------------|
| **Overview** | System stats (CPU, Memory, Disk) and game server instance grid |
| **Servers** | Table of all server instances with Start / Stop / Restart actions |
| **Server Detail** | Detailed view of a single server: status, match info, players, console |
| **Logs** | Real-time application log viewer |
| **Config** | Edit `hon_data` and `application_data` settings from the browser |

### Server Actions

| Action | Description |
|--------|-------------|
| **Start** | Start a stopped server instance |
| **Stop** | Stop a running server instance |
| **Restart** | Stop and start a server instance |
| **Enable** | Enable a server instance for auto-management |
| **Disable** | Disable a server instance (will not auto-restart) |

---

## DDoS Protection Proxy

When `man_enableProxy` is set to `true`, Energizer runs a built-in TCP/UDP reverse proxy in front of each game server:

- Game clients connect to **proxy port** (game port + 10000)
- The proxy forwards traffic to the actual game port on localhost
- Includes rate limiting and connection tracking
- Protects the real game server port from direct exposure

### Proxy Port Mapping

| Setting | Without Proxy | With Proxy |
|---------|--------------|------------|
| Game Port | `11235` | Client connects to `21235` |
| Voice Port | `11335` | Client connects to `21335` |

> **Important:** When proxy is enabled, make sure to open the proxy ports (game port + 10000) in your firewall instead of the base game ports.

---

## Ports to Open

Depending on your configuration, open the following ports in your firewall/router:

| Port | Protocol | Description |
|------|----------|-------------|
| `svr_api_port` (default: 5000) | TCP | REST API + Dashboard |
| `svr_managerPort` (default: 1134) | TCP | Game server manager communication |
| `svr_starting_gamePort` + N (default: 11235-11239) | TCP/UDP | Game server ports (N = `svr_total - 1`) |
| `svr_starting_voicePort` + N (default: 11335-11339) | UDP | Voice ports |
| Game ports + 10000 (if proxy enabled) | TCP/UDP | Proxy ports (e.g., 21235-21239) |
| Voice ports + 10000 (if proxy enabled) | UDP | Voice proxy ports (e.g., 21335-21339) |

---

## Troubleshooting

### Servers not visible in game lobby

1. Check that `svr_ip` is set to your correct public IP (or leave empty for auto-detect)
2. Verify the game ports are open in your firewall
3. Check the console for `Sent heartbeat` messages -- this confirms the server is communicating with the master server
4. If using proxy (`man_enableProxy: true`), make sure the proxy ports (game port + 10000) are open, not just the base game ports

### Dashboard shows "Unknown" status

- Rebuild and restart `energizer.exe` to ensure you're running the latest version
- The status updates when the game server connects to the master server and enters the `ready` state

### Server console window title changes to hex values

This is normal. After the game server registers with the master server, `hon_x64.exe` updates its console window title to include its session ID (e.g., `3 - 77BF555D`). This is not an Energizer issue.

### Bot matches not working

- Make sure `svr_allow_bot_matches` is set to `true` in `config/config.json`
- Restart Energizer after changing this setting

### Logs show "??:??:??" timestamps

- Rebuild and restart Energizer -- this was fixed in a recent update

### Build fails for Linux

- Ensure you have Go 1.22+ installed
- Cross-compile from Windows: `$env:GOOS="linux"; $env:GOARCH="amd64"; go build -o energizer ./cmd/energizer`

---
