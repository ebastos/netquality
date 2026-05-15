BINARY := netqualityd
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w

.PHONY: build build-pi build-pi-armv7 build-pi-arm64 build-armv7 test vet install enable clean

build:
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/netqualityd

# Cross-compile both Pi targets (pick the one matching `uname -m` on the Pi).
build-pi: build-pi-armv7 build-pi-arm64
	@echo ""
	@echo "Copy to the Pi as /usr/local/bin/netqualityd:"
	@echo "  armv7l  (32-bit OS):  bin/$(BINARY)-linux-armv7"
	@echo "  aarch64 (64-bit OS):  bin/$(BINARY)-linux-arm64"
	@echo "On the Pi, run: uname -m"

# 32-bit Raspberry Pi OS (armv7l) — most common on Pi 3/4 with legacy OS images
build-pi-armv7: build-armv7

build-armv7:
	GOOS=linux GOARCH=arm GOARM=7 go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY)-linux-armv7 ./cmd/netqualityd

# 64-bit Raspberry Pi OS (aarch64)
build-pi-arm64:
	GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY)-linux-arm64 ./cmd/netqualityd

test:
	go test ./...

vet:
	go vet ./...

install: build
	install -d /etc/netquality /var/lib/netquality
	install -m 0755 bin/$(BINARY) /usr/local/bin/$(BINARY)
	install -m 0644 deploy/config.example.yaml /etc/netquality/config.yaml
	install -m 0644 deploy/netqualityd.service /etc/systemd/system/netqualityd.service
	@echo "Run: sudo setcap cap_net_raw+ep /usr/local/bin/$(BINARY)  # if not using AmbientCapabilities"
	@echo "Run: sudo useradd -r -s /usr/sbin/nologin netquality 2>/dev/null || true"
	@echo "Run: sudo chown netquality:netquality /var/lib/netquality"
	@echo "Run: sudo systemctl daemon-reload && sudo systemctl enable --now netqualityd"

enable:
	sudo systemctl enable --now netqualityd

clean:
	rm -rf bin/
