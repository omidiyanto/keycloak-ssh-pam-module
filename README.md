# Keycloak SSH PAM — Device Flow Authentication

Modul PAM untuk autentikasi SSH via **Keycloak Device Authorization Flow** (RFC 8628).  
Login SSH tanpa password — cukup buka browser, login Keycloak + 2FA, dan masuk.

```
$ ssh john@server

  ╔══════════════════════════════════════════════════════╗
  ║         🔐 Keycloak SSH Authentication              ║
  ╚══════════════════════════════════════════════════════╝

  Complete your login in the browser:
  👉 https://sso.domainanda.com/realms/master/device?user_code=ABCD-EFGH

  Press ENTER after completing browser login: 

  ⏳ Verifying authentication with Keycloak...
  ✅ Authentication successful!

john@server:~$ 
```

## Features

- **🔐 Browser-based SSO** — Password tidak pernah diketik di terminal SSH
- **📱 2FA/MFA Support** — Semua fitur Keycloak (OTP, WebAuthn, Social Login) otomatis tersedia
- **🔄 Backchannel Logout** — Admin bisa kill sesi SSH dari Keycloak Admin Console
- **📝 Session Tracking** — Monitoring sesi aktif via HTTP API
- **🏗️ Pure Go** — Ditulis 100% Go (CGo untuk PAM interface), tanpa dependency C library

## Architecture

```
ssh user@server
      │
      ▼
  sshd → PAM → pam_keycloak_device.so (Go + CGo)
      │
      │  1. POST /auth/device → dapat URL + code
      │  2. Tampilkan URL ke terminal via pam_conv
      │  3. User login di browser + 2FA
      │  4. Poll token endpoint → sukses
      │  5. Simpan session → PAM_SUCCESS
      ▼
  Shell terbuka!

  ┌──────────────────────────────────────────┐
  │  keycloak-ssh-monitor (systemd daemon)   │
  │  POST /backchannel-logout ← Keycloak     │
  │  Parse JWT → kill SSH PID → session end  │
  └──────────────────────────────────────────┘
```

## Quick Start

### Prerequisites

- Linux server (Debian/Ubuntu atau RHEL/CentOS)
- Go 1.21+
- Keycloak 20+ dengan akses admin
- `libpam0g-dev` (Debian) atau `pam-devel` (RHEL)

### 1. Build & Install

```bash
# Clone
git clone <repo-url>
cd keycloak-pam-ssh-linux

# Install (requires root)
sudo ./scripts/install.sh
```

Atau manual:

```bash
# Install dependencies (Debian/Ubuntu)
sudo apt-get install -y golang libpam0g-dev build-essential

# Build
make build

# Install
sudo make install
```

### 2. Configure Keycloak

Ikuti panduan lengkap: [docs/keycloak-setup.md](docs/keycloak-setup.md)

Ringkasan:
1. Buat Client `ssh-pam-client`
2. Enable **OAuth 2.0 Device Authorization Grant**
3. Set **Backchannel Logout URL**: `http://<server-ip>:7291/backchannel-logout`
4. Enable **Backchannel logout session required**

### 3. Configure Server

Edit config:
```bash
sudo nano /etc/keycloak-ssh/config.yaml
```

```yaml
keycloak:
  server_url: "https://sso.domainanda.com"
  realm: "master"
  client_id: "ssh-pam-client"
```

### 4. Configure PAM

Edit `/etc/pam.d/sshd`:
```bash
# Keycloak Device Flow Authentication  
auth    required    pam_keycloak_device.so
auth    required    pam_nologin.so

# Standard
account required    pam_unix.so
session required    pam_keycloak_device.so
session required    pam_unix.so
session optional    pam_motd.so
```

### 5. Configure SSHD

Edit `/etc/ssh/sshd_config`:
```
KbdInteractiveAuthentication yes
UsePAM yes
PasswordAuthentication no
ChallengeResponseAuthentication yes
AuthenticationMethods keyboard-interactive
```

### 6. Start & Test

```bash
# Start monitor daemon
sudo systemctl enable --now keycloak-ssh-monitor

# Restart SSH (⚠️ keep an existing session open!)
sudo systemctl restart sshd

# Test from another terminal
ssh john@server
```

## Monitor Daemon API

| Endpoint | Method | Keterangan |
|----------|--------|-----------|
| `/backchannel-logout` | POST | Keycloak webhook — terima logout token |
| `/healthz` | GET | Health check |
| `/sessions` | GET | List active SSH sessions |

```bash
# Health check
curl http://localhost:7291/healthz

# List sessions
curl http://localhost:7291/sessions
```

## Configuration

Custom config path via PAM argument:
```
auth required pam_keycloak_device.so config=/path/to/config.yaml
```

Lihat [configs/keycloak-ssh.yaml](configs/keycloak-ssh.yaml) untuk reference lengkap semua option.

## Project Structure

```
.
├── pam_module/               # PAM module (→ pam_keycloak_device.so)
│   ├── main.go               # Empty main (c-shared requirement)
│   ├── pam_entry.go           # CGo PAM entry points
│   ├── pam_conv.go            # CGo PAM conversation helpers
│   └── auth.go                # Device flow orchestration (pure Go)
│
├── cmd/monitor/               # Monitor daemon (→ keycloak-ssh-monitor)
│   └── main.go
│
├── internal/                  # Shared libraries
│   ├── config/config.go       # YAML config parser
│   ├── deviceflow/deviceflow.go  # Keycloak Device Flow client
│   ├── session/store.go       # Session file management
│   └── logout/handler.go      # Backchannel logout handler
│
├── configs/                   # Configuration templates
│   ├── keycloak-ssh.yaml
│   └── systemd/keycloak-ssh-monitor.service
│
├── scripts/install.sh         # Auto-installer
├── docs/keycloak-setup.md     # Keycloak setup guide
├── Makefile
└── README.md
```

## Security Considerations

- PAM module runs as **root** (loaded by sshd) — binary is owned by root, mode 0755
- Session files stored with mode **0600** in a **0700** directory
- Monitor daemon uses **systemd hardening** (NoNewPrivileges, ProtectSystem, etc.)
- Logout token JWT is **not signature-verified** — relies on network trust (Keycloak → server)
  - For production: use TLS between Keycloak and monitor daemon
- No credentials stored on disk — PAM module communicates with Keycloak per-session

## Troubleshooting

```bash
# Check PAM module logs
sudo journalctl -t pam_keycloak_device -f

# Check monitor daemon logs
sudo journalctl -u keycloak-ssh-monitor -f

# Check sshd logs
sudo journalctl -u sshd -f

# Verify PAM module is loadable
sudo ldd /lib/security/pam_keycloak_device.so

# Test Keycloak connectivity
curl -s "https://sso.domainanda.com/realms/master/.well-known/openid-configuration" | jq .device_authorization_endpoint
```

## License

MIT