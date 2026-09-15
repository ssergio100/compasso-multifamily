.PHONY: all build build-agent build-agent-portable build-server admin-ui-dev package-client package-deb package-server package-all publish-admin-ui publish-client-release publish-server test-deb test-server-package fmt fmt-check lint test test-go test-ui test-admin-ui test-migrations test-backup-restore test-security test-docs clean

all: test

build: build-agent build-server
	go build ./...

build-agent:
	mkdir -p bin
	go build -o bin/tempo-agent ./agent/cmd/tempo-agent
	go build -o bin/tempo-agent-configure ./agent/cmd/tempo-agent-configure

build-agent-portable:
	./scripts/build-portable-client-binaries.sh

build-server:
	mkdir -p bin
	go build -o bin/tempo-server ./server/cmd/tempo-server

admin-ui-dev:
	cd admin-ui && npm run dev

package-client: package-deb

package-deb: build-agent-portable
	./scripts/build-debian-package.sh

package-server:
	./scripts/build-server-package.sh

package-all:
	./scripts/build-all-debian-packages.sh

publish-admin-ui:
	./scripts/publish-admin-ui.sh

publish-client-release:
	./scripts/publish-client-release.sh

publish-server:
	./scripts/publish-server.sh

test-deb: package-deb
	./scripts/test-debian-package.sh

test-server-package:
	./scripts/test-server-package.sh

fmt:
	gofmt -w $$(find agent server protocol -type f -name '*.go' 2>/dev/null)

fmt-check:
	@unformatted="$$(gofmt -l $$(find agent server protocol -type f -name '*.go' 2>/dev/null))"; \
	if [ -n "$$unformatted" ]; then \
		echo "Arquivos Go sem formatação:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi

lint: fmt-check
	go vet ./...

test-go:
	go test ./...

test-ui:
	cd local-ui && python3 -m unittest discover -v

test-admin-ui:
	cd admin-ui && npm run typecheck
	cd admin-ui && npm run build

test-migrations:
	./scripts/test-migrations.sh

test-backup-restore:
	./scripts/test-backup-restore.sh

test-security:
	./scripts/test-security-packaging.sh

test-docs:
	./scripts/check-doc-links.sh

test: lint test-go test-ui test-admin-ui test-migrations test-backup-restore test-security test-docs build

clean:
	rm -rf ./bin ./dist
