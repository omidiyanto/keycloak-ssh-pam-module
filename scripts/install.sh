#!/usr/bin/env bash
# ==============================================================================
# Keycloak SSH PAM — Quick Installer
# ==============================================================================
# Usage:
#   sudo ./scripts/install.sh
#
# This script:
#   1. Installs build dependencies
#   2. Builds the PAM module and monitor daemon
#   3. Installs binaries and config files
#   4. Creates necessary directories
#   5. Shows next steps
# ==============================================================================

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# Paths
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
CONFIG_DIR="/etc/keycloak-ssh"
SESSION_DIR="/var/run/keycloak-ssh/sessions"

log_info()  { echo -e "${GREEN}[INFO]${NC}  $*"; }
log_warn()  { echo -e "${YELLOW}[WARN]${NC}  $*"; }
log_error() { echo -e "${RED}[ERROR]${NC} $*"; }
log_step()  { echo -e "\n${CYAN}▶ $*${NC}"; }

# Check root
if [[ $EUID -ne 0 ]]; then
    log_error "This script must be run as root (sudo)"
    exit 1
fi

echo ""
echo "╔══════════════════════════════════════════════════════════╗"
echo "║     Keycloak SSH PAM — Installer                        ║"
echo "╚══════════════════════════════════════════════════════════╝"
echo ""

# ============================================================================
# Step 1: Install Dependencies
# ============================================================================
log_step "Checking build dependencies..."

install_deps_debian() {
    apt-get update -qq
    apt-get install -y -qq golang libpam0g-dev build-essential > /dev/null
}

install_deps_rhel() {
    dnf install -y golang pam-devel gcc make > /dev/null 2>&1 || \
    yum install -y golang pam-devel gcc make > /dev/null 2>&1
}

if command -v apt-get &> /dev/null; then
    log_info "Detected Debian/Ubuntu — installing dependencies..."
    install_deps_debian
elif command -v dnf &> /dev/null || command -v yum &> /dev/null; then
    log_info "Detected RHEL/CentOS — installing dependencies..."
    install_deps_rhel
else
    log_warn "Unknown distro — please install Go, PAM dev headers, and GCC manually"
fi

# Check Go version
if ! command -v go &> /dev/null; then
    log_error "Go is not installed. Please install Go 1.21+ and try again."
    exit 1
fi
GO_VERSION=$(go version | grep -oP '[\d]+\.[\d]+' | head -1)
log_info "Go version: $GO_VERSION"

# ============================================================================
# Step 2: Build
# ============================================================================
log_step "Building binaries..."

cd "$PROJECT_DIR"

# Download Go dependencies
go mod download

# Build PAM module
log_info "Building PAM module..."
make pam

# Build monitor daemon
log_info "Building monitor daemon..."
make monitor

log_info "Build successful!"

# ============================================================================
# Step 3: Install
# ============================================================================
log_step "Installing components..."

# Detect PAM module directory
if [ -d "/lib/x86_64-linux-gnu/security" ]; then
    PAM_DIR="/lib/x86_64-linux-gnu/security"
elif [ -d "/lib64/security" ]; then
    PAM_DIR="/lib64/security"
else
    PAM_DIR="/lib/security"
fi

# Install PAM module
install -m 0755 build/pam_keycloak_device.so "$PAM_DIR/pam_keycloak_device.so"
log_info "PAM module installed  → $PAM_DIR/pam_keycloak_device.so"

# Install monitor daemon
install -m 0755 build/keycloak-ssh-monitor /usr/local/bin/keycloak-ssh-monitor
log_info "Monitor daemon        → /usr/local/bin/keycloak-ssh-monitor"

# Create config directory
mkdir -p "$CONFIG_DIR"
if [ ! -f "$CONFIG_DIR/config.yaml" ]; then
    install -m 0600 configs/keycloak-ssh.yaml "$CONFIG_DIR/config.yaml"
    log_info "Config template       → $CONFIG_DIR/config.yaml"
else
    log_warn "Config exists         → $CONFIG_DIR/config.yaml (not overwritten)"
fi

# Create session directory
mkdir -p "$SESSION_DIR"
chmod 0700 "$SESSION_DIR"
log_info "Session directory     → $SESSION_DIR"

# Install systemd service
install -m 0644 configs/systemd/keycloak-ssh-monitor.service /etc/systemd/system/
systemctl daemon-reload
log_info "Systemd service       → /etc/systemd/system/keycloak-ssh-monitor.service"

# ============================================================================
# Step 4: Show Next Steps
# ============================================================================
echo ""
echo "╔══════════════════════════════════════════════════════════╗"
echo "║  ✅ Installation Complete!                               ║"
echo "╚══════════════════════════════════════════════════════════╝"
echo ""
echo "  Next steps:"
echo ""
echo "  1. Configure Keycloak (see docs/keycloak-setup.md)"
echo ""
echo "  2. Edit the config file:"
echo "     ${CYAN}sudo nano $CONFIG_DIR/config.yaml${NC}"
echo ""
echo "  3. Configure PAM — add to /etc/pam.d/sshd:"
echo "     ${CYAN}# Comment out existing auth lines, then add:${NC}"
echo "     ${GREEN}auth    required    pam_keycloak_device.so${NC}"
echo "     ${GREEN}auth    required    pam_nologin.so${NC}"
echo ""
echo "     ${CYAN}# Add session tracking:${NC}"
echo "     ${GREEN}session required    pam_keycloak_device.so${NC}"
echo ""
echo "  4. Configure SSHD — edit /etc/ssh/sshd_config:"
echo "     ${GREEN}KbdInteractiveAuthentication yes${NC}"
echo "     ${GREEN}UsePAM yes${NC}"
echo "     ${GREEN}PasswordAuthentication no${NC}"
echo "     ${GREEN}ChallengeResponseAuthentication yes${NC}"
echo "     ${GREEN}AuthenticationMethods keyboard-interactive${NC}"
echo ""
echo "  5. Start the monitor daemon:"
echo "     ${CYAN}sudo systemctl enable --now keycloak-ssh-monitor${NC}"
echo ""
echo "  6. Restart SSH (⚠️  keep an existing session open!):"
echo "     ${CYAN}sudo systemctl restart sshd${NC}"
echo ""
echo "  7. Test from another terminal:"
echo "     ${CYAN}ssh user@this-server${NC}"
echo ""
