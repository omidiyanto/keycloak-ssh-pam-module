# ============================================================================
# Keycloak PAM SSH — Build System
# ============================================================================
# Builds:
#   1. pam_keycloak_device.so — PAM module (C shared library via CGo)
#   2. keycloak-ssh-monitor   — Backchannel logout daemon
# ============================================================================

.PHONY: all build pam monitor clean install uninstall help

# Build output directory
BUILD_DIR := build

# Binary names
PAM_MODULE  := $(BUILD_DIR)/pam_keycloak_device.so
MONITOR_BIN := $(BUILD_DIR)/keycloak-ssh-monitor

# Installation paths
PAM_INSTALL_DIR     := /lib/security
MONITOR_INSTALL_DIR := /usr/local/bin
CONFIG_DIR          := /etc/keycloak-ssh
SESSION_DIR         := /var/run/keycloak-ssh/sessions
SYSTEMD_DIR         := /etc/systemd/system

# Go build flags
GO        := go
CGO_FLAGS := CGO_ENABLED=1
LDFLAGS   := -s -w

# Detect PAM library path (Debian/Ubuntu vs RHEL/CentOS)
ifneq ($(wildcard /lib/x86_64-linux-gnu/security),)
    PAM_INSTALL_DIR := /lib/x86_64-linux-gnu/security
else ifneq ($(wildcard /lib64/security),)
    PAM_INSTALL_DIR := /lib64/security
endif

# ============================================================================
# Targets
# ============================================================================

## all: Build both PAM module and monitor daemon
all: build

## build: Build all binaries
build: pam monitor
	@echo ""
	@echo "✅ Build complete!"
	@echo "   PAM module:  $(PAM_MODULE)"
	@echo "   Monitor:     $(MONITOR_BIN)"
	@echo ""

## pam: Build the PAM module (.so)
pam: $(BUILD_DIR)
	@echo "🔨 Building PAM module..."
	$(CGO_FLAGS) $(GO) build -buildmode=c-shared \
		-ldflags="$(LDFLAGS)" \
		-o $(PAM_MODULE) \
		./pam_module/
	@# Remove the auto-generated .h file (not needed)
	@rm -f $(BUILD_DIR)/pam_keycloak_device.h

## monitor: Build the monitor daemon
monitor: $(BUILD_DIR)
	@echo "🔨 Building monitor daemon..."
	$(CGO_FLAGS) $(GO) build \
		-ldflags="$(LDFLAGS)" \
		-o $(MONITOR_BIN) \
		./cmd/monitor/

$(BUILD_DIR):
	@mkdir -p $(BUILD_DIR)

## clean: Remove build artifacts
clean:
	@echo "🧹 Cleaning build artifacts..."
	rm -rf $(BUILD_DIR)

## install: Install all components to system paths (requires root)
install: build
	@echo ""
	@echo "📦 Installing Keycloak SSH PAM module..."
	@echo ""

	@# Install PAM module
	install -m 0755 $(PAM_MODULE) $(PAM_INSTALL_DIR)/pam_keycloak_device.so
	@echo "   ✅ PAM module  → $(PAM_INSTALL_DIR)/pam_keycloak_device.so"

	@# Install monitor daemon
	install -m 0755 $(MONITOR_BIN) $(MONITOR_INSTALL_DIR)/keycloak-ssh-monitor
	@echo "   ✅ Monitor     → $(MONITOR_INSTALL_DIR)/keycloak-ssh-monitor"

	@# Create config directory and install config template
	mkdir -p $(CONFIG_DIR)
	@if [ ! -f $(CONFIG_DIR)/config.yaml ]; then \
		install -m 0600 configs/keycloak-ssh.yaml $(CONFIG_DIR)/config.yaml; \
		echo "   ✅ Config      → $(CONFIG_DIR)/config.yaml (new)"; \
	else \
		echo "   ⏭️  Config      → $(CONFIG_DIR)/config.yaml (exists, not overwritten)"; \
	fi

	@# Create session directory
	mkdir -p $(SESSION_DIR)
	chmod 0700 $(SESSION_DIR)
	@echo "   ✅ Session dir  → $(SESSION_DIR)"

	@# Install systemd service
	install -m 0644 configs/systemd/keycloak-ssh-monitor.service $(SYSTEMD_DIR)/
	systemctl daemon-reload
	@echo "   ✅ Systemd     → $(SYSTEMD_DIR)/keycloak-ssh-monitor.service"

	@echo ""
	@echo "══════════════════════════════════════════════════════════"
	@echo "  Installation complete!"
	@echo "══════════════════════════════════════════════════════════"
	@echo ""
	@echo "  Next steps:"
	@echo "  1. Edit config:       sudo nano $(CONFIG_DIR)/config.yaml"
	@echo "  2. Configure PAM:     sudo nano /etc/pam.d/sshd"
	@echo "  3. Configure SSHD:    sudo nano /etc/ssh/sshd_config"
	@echo "  4. Start monitor:     sudo systemctl enable --now keycloak-ssh-monitor"
	@echo "  5. Restart SSH:       sudo systemctl restart sshd"
	@echo ""
	@echo "  See docs/keycloak-setup.md for Keycloak configuration."
	@echo ""

## uninstall: Remove all installed components (requires root)
uninstall:
	@echo "🗑️  Uninstalling Keycloak SSH PAM module..."
	systemctl stop keycloak-ssh-monitor 2>/dev/null || true
	systemctl disable keycloak-ssh-monitor 2>/dev/null || true
	rm -f $(PAM_INSTALL_DIR)/pam_keycloak_device.so
	rm -f $(MONITOR_INSTALL_DIR)/keycloak-ssh-monitor
	rm -f $(SYSTEMD_DIR)/keycloak-ssh-monitor.service
	systemctl daemon-reload
	@echo ""
	@echo "   ⚠️  Config dir preserved: $(CONFIG_DIR)"
	@echo "   ⚠️  Session dir preserved: $(SESSION_DIR)"
	@echo "   ⚠️  Remember to restore /etc/pam.d/sshd and /etc/ssh/sshd_config"
	@echo ""

## deps: Install build dependencies (Debian/Ubuntu)
deps:
	@echo "📥 Installing build dependencies..."
	apt-get update
	apt-get install -y golang libpam0g-dev build-essential
	@echo "✅ Dependencies installed"

## help: Show this help
help:
	@echo "Keycloak PAM SSH — Build Targets:"
	@echo ""
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## /  /' | column -t -s ':'
	@echo ""
