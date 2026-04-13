# planning.md: Proyek Custom PAM Keycloak Device Flow & Session Manager

## Tujuan Utama
Membangun sistem otentikasi SSH Linux terpusat menggunakan Keycloak dengan skema "Device Authorization Grant" (menampilkan URL di terminal), dilengkapi dengan fitur pemutusan sesi paksa (Backchannel Logout) oleh administrator Keycloak.

## Arsitektur Sistem
Sistem akan terdiri dari dua komponen utama:
1. `pam_keycloak_device.so`: Modul PAM (ditulis dalam C) untuk interaksi login.
2. `keycloak-ssh-monitor`: Daemon Service (disarankan ditulis dalam Golang/Python untuk kemudahan HTTP/JSON) untuk mengurus Backchannel Logout.

---

## Fase 1: Persiapan & Infrastruktur (Minggu 1)
- [ ] **Setup Keycloak Server:**
  - Buat Realm dan Client (`ssh-client`).
  - Aktifkan `OAuth 2.0 Device Authorization Grant`.
  - Konfigurasi URL untuk Backchannel Logout (nantinya mengarah ke endpoint `keycloak-ssh-monitor`).
- [ ] **Persiapan Environtment C (Ubuntu/Debian):**
  - Install dependencies: `libcurl4-openssl-dev`, `libpam0g-dev`, `libjansson-dev` (atau `cJSON` untuk parsing JSON).
  - Siapkan Makefile standar untuk *compile* kode C menjadi *Shared Object* (`.so`).

## Fase 2: Pengembangan Modul PAM - Device Flow (Minggu 2)
Fokus: Membuat modul PAM yang bisa berkomunikasi dengan API Keycloak dan berinteraksi dengan layar terminal (MobaXterm).
- [ ] **Fungsi pam_sm_authenticate():**
  - Hit endpoint Keycloak: `POST /realms/{realm}/protocol/openid-connect/auth/device`.
  - Parse JSON response untuk mendapatkan `device_code`, `user_code`, `verification_uri`, dan `interval`.
- [ ] **Interaksi Terminal (PAM Conversation):**
  - Gunakan `pam_get_item(pamh, PAM_CONV, ...)` untuk mencetak pesan ke layar user.
  - Tampilkan instruksi: *"Buka URL: [verification_uri] dan masukkan kode: [user_code]"*.
- [ ] **Polling Token (Loop):**
  - Buat *looping* (menggunakan `sleep` sesuai `interval` dari Keycloak).
  - Hit endpoint Keycloak: `POST /realms/{realm}/protocol/openid-connect/token` dengan parameter `grant_type=urn:ietf:params:oauth:grant-type:device_code`.
  - Berhenti dan kembalikan `PAM_SUCCESS` jika mendapat Access Token.
  - Berhenti dan kembalikan `PAM_AUTH_ERR` jika *timeout* atau ditolak.

## Fase 3: Pencatatan Sesi / Session Tracking (Minggu 3)
Fokus: Menyambungkan keberhasilan modul PAM dengan Daemon Monitor agar bisa di-*logout* nantinya.
- [ ] **Fungsi pam_sm_open_session():**
  - Setelah login berhasil, ambil ID unik dari Keycloak (misal `session_state` atau `sub/username` dari token).
  - Dapatkan PID (Process ID) dari *shell* SSH saat ini (bisa dilacak dari *parent process* atau `/proc`).
  - Tulis mapping ini ke sebuah file atau SQLite kecil di `/var/run/pam_keycloak/`. Format: `[Keycloak_Session_ID] -> [Linux_Username] -> [SSH_PID]`.
- [ ] **Fungsi pam_sm_close_session():**
  - Hapus data *mapping* dari file/SQLite tersebut jika user *logout* secara normal (ketik `exit` di MobaXterm).

## Fase 4: Pengembangan Companion Daemon - Backchannel Logout (Minggu 4)
Fokus: Membuat *service background* untuk memantau pemutusan sesi.
- [ ] **Pilih Metode Terminasi:**
  - *Opsi A (Webhook / OIDC Standard):* Daemon membuka port HTTP internal (misal port 8080). Keycloak disetting agar mengirim HTTP POST (Backchannel Logout Token) ke IP server ini saat Admin klik "Logout".
  - *Opsi B (Polling / Lebih aman dari sisi firewall):* Daemon berjalan tiap 1 menit mengecek API Keycloak `/realms/{realm}/protocol/openid-connect/token/introspect` untuk setiap sesi yang aktif di `/var/run/pam_keycloak/`.
- [ ] **Eksekusi Terminasi:**
  - Jika sesi dinyatakan "Mati/Revoked" oleh Keycloak, daemon membaca file *mapping*.
  - Eksekusi perintah *kill*: `kill -HUP [SSH_PID]` atau `pkill -u [Linux_Username] sshd`.
- [ ] **Systemd Integration:**
  - Buat file `keycloak-ssh-monitor.service` agar daemon berjalan otomatis saat server *booting*.

## Fase 5: Testing, Hardening & Deployment (Minggu 5)
- [ ] **Test Skenario:**
  - Login berhasil via *browser* di PC lokal.
  - Login gagal (salah masukin kode / *timeout*).
  - Admin Keycloak mematikan sesi (*End Session*) -> Sesi MobaXterm harus langsung terputus (*Connection Closed*).
- [ ] **Security Hardening:**
  - Pastikan modul C aman dari *memory leak* (*buffer overflow*).
  - Pastikan hak akses direktori file *mapping* PID hanya bisa dibaca/tulis oleh `root` agar tidak disalahgunakan *user* biasa.