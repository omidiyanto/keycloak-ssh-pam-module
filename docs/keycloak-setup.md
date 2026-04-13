# Keycloak Setup Guide — SSH Device Flow Authentication

Panduan lengkap untuk mengkonfigurasi Keycloak agar bekerja dengan PAM module SSH Device Flow.

## Prasyarat

- Keycloak 20+ (atau Red Hat SSO 7.6+) sudah berjalan
- Akses Administrator ke Keycloak Admin Console
- Server SSH target sudah bisa dijangkau dari Keycloak (untuk backchannel logout)

---

## 1. Buat atau Pilih Realm

Gunakan realm yang sudah ada, atau buat realm baru:

1. Login ke **Keycloak Admin Console** (`https://sso.domainanda.com/admin/`)
2. Klik dropdown realm di pojok kiri atas
3. Klik **Create Realm**
4. Isi **Realm name**: `master` (atau nama lain sesuai kebutuhan)
5. Klik **Create**

---

## 2. Buat Client untuk SSH

Client ini adalah representasi dari server SSH di Keycloak.

### 2.1. Buat Client Baru

1. Navigasi ke **Clients** → **Create client**
2. Isi:
   - **Client type**: `OpenID Connect`
   - **Client ID**: `ssh-pam-client`
   - **Name**: `SSH PAM Authentication`
   - **Description**: `OAuth2 Device Flow for SSH login via PAM`
3. Klik **Next**

### 2.2. Capability Config

Pada halaman kedua:
- **Client authentication**: **OFF** (public client)
  - *Jika kamu butuh confidential client, nyalakan dan catat client secret-nya*
- **Authorization**: OFF
- **Authentication flow**:
  - ☑️ **Standard flow**: OFF (tidak diperlukan)
  - ☑️ **Direct access grants**: OFF (tidak diperlukan)
  - ☑️ **OAuth 2.0 Device Authorization Grant**: **ON** ✅ ← **KRITIS!**
  - ☑️ **Service accounts roles**: OFF

4. Klik **Next**, lalu **Save**

### 2.3. Konfigurasi Logout

Setelah client dibuat, masuk ke tab **Settings** → scroll ke bagian **Logout settings**:

- **Backchannel logout URL**: 
  ```
  http://<IP-SERVER-SSH>:7291/backchannel-logout
  ```
  ⚠️ Ganti `<IP-SERVER-SSH>` dengan IP/hostname server SSH target.
  
  Jika ada **multiple SSH server**, perlu client terpisah per server atau gunakan reverse proxy.

- **Backchannel logout session required**: **ON** ✅ ← **KRITIS!**
  
  Ini memastikan `sid` (session ID) disertakan dalam logout token, sehingga monitor daemon bisa mengetahui sesi mana yang harus dimatikan.

- **Front channel logout**: OFF

### 2.4. Konfigurasi Token (Opsional)

Masuk ke **Realm Settings** → **Tokens** tab:

| Setting | Recommended Value | Keterangan |
|---------|-------------------|------------|
| OAuth 2.0 Device Code Lifespan | `600` (10 menit) | Berapa lama user punya waktu untuk login di browser |
| OAuth 2.0 Device Polling Interval | `5` (detik) | Interval polling PAM module |
| Access Token Lifespan | `3600` (1 jam) | Opsional, hanya untuk session tracking |

---

## 3. Buat User Test

1. Navigasi ke **Users** → **Add user**
2. Isi:
   - **Username**: `john` (harus **sama persis** dengan username Linux di server SSH)
   - **Email**: `john@domainanda.com`
   - **First name**: `John`
   - **Last name**: `Doe`
   - **Email verified**: ON
3. Klik **Create**

### 3.1. Set Password

1. Masuk ke tab **Credentials**
2. Klik **Set password**
3. Isi password dan set **Temporary** ke OFF
4. Klik **Save**

### 3.2. Setup 2FA (OTP) — Opsional tapi Direkomendasikan

Untuk memaksa OTP pada semua user:

1. Navigasi ke **Authentication** → **Required Actions**
2. Aktifkan **Configure OTP** dan set sebagai **Default Action**

Atau per-user:
1. Masuk ke user → tab **Credentials**
2. User akan diminta setup OTP saat login pertama kali

---

## 4. Verifikasi Konfigurasi

### 4.1. Test Device Flow Manual

Dari terminal mana saja, test endpoint device authorization:

```bash
# Request device code
curl -s -X POST \
  "https://sso.domainanda.com/realms/master/protocol/openid-connect/auth/device" \
  -d "client_id=ssh-pam-client" \
  -d "scope=openid" | jq .
```

**Expected response:**
```json
{
  "device_code": "...",
  "user_code": "ABCD-EFGH",
  "verification_uri": "https://sso.domainanda.com/realms/master/device",
  "verification_uri_complete": "https://sso.domainanda.com/realms/master/device?user_code=ABCD-EFGH",
  "expires_in": 600,
  "interval": 5
}
```

Lalu buka `verification_uri_complete` di browser dan login.

### 4.2. Test Token Polling

```bash
# Setelah login di browser, poll token:
curl -s -X POST \
  "https://sso.domainanda.com/realms/master/protocol/openid-connect/token" \
  -d "grant_type=urn:ietf:params:oauth:grant-type:device_code" \
  -d "device_code=<DEVICE_CODE_DARI_STEP_SEBELUMNYA>" \
  -d "client_id=ssh-pam-client" | jq .
```

**Expected response (setelah login):**
```json
{
  "access_token": "eyJ...",
  "token_type": "Bearer",
  "expires_in": 3600,
  "session_state": "abc-123-def-456",
  ...
}
```

### 4.3. Test Backchannel Logout (Opsional)

Verifikasi bahwa monitor daemon bisa menerima logout token:

```bash
# Check health
curl http://<IP-SERVER-SSH>:7291/healthz

# Check active sessions
curl http://<IP-SERVER-SSH>:7291/sessions
```

---

## 5. Troubleshooting

### Device Flow tidak aktif

```
{"error":"unauthorized_client","error_description":"Client not allowed for device grant"}
```

**Solusi**: Pastikan **OAuth 2.0 Device Authorization Grant** di-enable pada client settings.

### Backchannel logout tidak mengirim `sid`

**Solusi**: Pastikan **Backchannel logout session required** di-ON-kan pada client settings.

### User tidak ditemukan

```
PAM: PAM_USER_UNKNOWN
```

**Solusi**: Username di Keycloak **harus sama persis** dengan username Linux (`/etc/passwd`). Keycloak username `john` → Linux user `john`.

### Connection refused pada backchannel logout

**Solusi**: 
- Pastikan `keycloak-ssh-monitor` berjalan: `systemctl status keycloak-ssh-monitor`
- Pastikan port 7291 terbuka di firewall: `sudo ufw allow 7291/tcp`
- Pastikan Keycloak bisa reach IP server SSH

---

## 6. Architecture Diagram

```
┌────────────────────────────┐
│     Keycloak Server        │
│  sso.domainanda.com        │
│                            │
│  Realm: master             │
│  Client: ssh-pam-client    │
│  Device Grant: ON          │
│  Backchannel Logout: ON    │
└──────┬────────────┬────────┘
       │            │
       │ Device     │ Backchannel
       │ Flow       │ Logout POST
       │            │
       ▼            ▼
┌────────────────────────────┐
│     SSH Server (Linux)     │
│                            │
│  PAM: pam_keycloak_device  │
│    → /lib/security/*.so    │
│                            │
│  Monitor: keycloak-ssh-mon │
│    → localhost:7291        │
│    → POST /backchannel-    │
│      logout                │
│                            │
│  Sessions:                 │
│    → /var/run/keycloak-ssh │
└────────────────────────────┘
```
