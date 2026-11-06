PREFIX ?= $(HOME)/.local
TAGS := webkit2_41

build:
	wails build -tags $(TAGS) -trimpath -ldflags "-s -w -X main.version=$$(git describe --tags --always --dirty)"

dev:
	wails dev -tags $(TAGS)

test:
	go test -tags $(TAGS) ./...

install: build
	install -Dm755 build/bin/dbro $(PREFIX)/bin/dbro
	install -Dm644 dbro.desktop $(PREFIX)/share/applications/dbro.desktop
	install -Dm644 build/appicon.png $(PREFIX)/share/icons/hicolor/1024x1024/apps/dbro.png
	-update-desktop-database $(PREFIX)/share/applications 2>/dev/null

screenshots:
	scripts/screenshots.sh

uninstall:
	rm -f $(PREFIX)/bin/dbro $(PREFIX)/share/applications/dbro.desktop $(PREFIX)/share/icons/hicolor/1024x1024/apps/dbro.png

.PHONY: build dev test install uninstall screenshots
