SHA256 ?= $(shell command -v sha256sum >/dev/null 2>&1 && echo sha256sum || echo 'shasum -a 256')
CTR_IMAGE_GO = ghcr.io/cloudboss/docker.io/library/golang:1.26.2-bookworm
CTR_IMAGE_GOLANGCI = ghcr.io/cloudboss/golangci/golangci-lint:v2.11.4-alpine
VERSION ?=
OS ?= $(shell uname -s | tr '[:upper:]' '[:lower:]')
ARCH ?= $(shell uname -m | sed -e 's/x86_64/amd64/' -e 's/aarch64/arm64/')
DIR_OUT ?= _output
DIR_ROOT = $(realpath $(CURDIR))
DIR_OUT_ABS = $(abspath $(DIR_OUT))
DIR_STG = $(DIR_OUT)/staging/unobin/$(VERSION)/$(OS)/$(ARCH)
DIR_RELEASE ?= $(DIR_OUT)/release
ARCHIVE = unobin-$(VERSION)-$(OS)-$(ARCH).tar.gz
USER_ID = $(shell id -u)
GROUP_ID = $(shell id -g)
BUILD_IMAGE_HASH = $(shell { \
	printf '%s\n' '$(CTR_IMAGE_GO)' '$(USER_ID)' '$(GROUP_ID)'; \
	cat Containerfile.build; \
	} | $(SHA256) | cut -c 1-40)
CTR_IMAGE_LOCAL = unobin-build:$(BUILD_IMAGE_HASH)
HAS_IMAGE_LOCAL = $(DIR_OUT)/.image-local-$(BUILD_IMAGE_HASH)

CTR_RUN = docker run --rm \
	-v "$(DIR_ROOT):$(DIR_ROOT):z" \
	-w "$(DIR_ROOT)" \
	-e GOPATH="$(DIR_OUT_ABS)/go" \
	-e GOCACHE="$(DIR_OUT_ABS)/gocache" \
	-e XDG_CACHE_HOME="$(DIR_OUT_ABS)/cache" \
	-e npm_config_cache="$(DIR_OUT_ABS)/cache/npm" \
	-e CGO_ENABLED=0

export VERSION OS ARCH

.DEFAULT_GOAL := test

.PHONY: test lint check-docker check-version check-release check-platform release-one release \
	check-archive check-cli-version check-release-assets \
	release-linux-amd64 release-linux-arm64 release-darwin-amd64 release-darwin-arm64

check-docker:
	@command -v docker >/dev/null 2>&1 || { echo 'docker is required' >&2; exit 1; }

$(HAS_IMAGE_LOCAL): Containerfile.build | check-docker
	mkdir -p "$(DIR_OUT)"
	docker build \
		--build-arg FROM="$(CTR_IMAGE_GO)" \
		--build-arg USER_ID="$(USER_ID)" \
		--build-arg GROUP_ID="$(GROUP_ID)" \
		-f Containerfile.build -t "$(CTR_IMAGE_LOCAL)" .
	touch "$@"

test: | $(HAS_IMAGE_LOCAL)
	$(CTR_RUN) "$(CTR_IMAGE_LOCAL)" sh -c 'go vet ./... && go test -timeout 30m ./...'

lint: | check-docker
	mkdir -p "$(DIR_OUT)"
	$(CTR_RUN) -u "$(USER_ID):$(GROUP_ID)" \
		-e GOLANGCI_LINT_CACHE="$(DIR_OUT_ABS)/cache/golangci-lint" \
		"$(CTR_IMAGE_GOLANGCI)" golangci-lint run --timeout 5m ./...

check-version:
	@number='(0|[1-9][0-9]*)'; \
	prerelease='(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)'; \
	printf '%s\n' "$$VERSION" | LC_ALL=C grep -Eq \
		"^v$$number\.$$number\.$$number(-$$prerelease(\.$$prerelease)*)?$$" || \
		{ echo 'VERSION must be vMAJOR.MINOR.PATCH with an optional prerelease suffix' >&2; exit 1; }

check-release: check-version
	@case "$$VERSION" in \
		*-*) echo 'Release versions must be vMAJOR.MINOR.PATCH without a suffix' >&2; exit 1 ;; \
	esac
	@awk -v version="$$VERSION" \
		'$$1 == "##" && $$2 == "[" substr(version, 2) "]" && $$3 == "-" && \
		$$4 ~ /^[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]$$/ { found = 1 } \
		END { exit !found }' CHANGELOG.md || \
		{ echo "CHANGELOG.md needs a dated entry for $$VERSION" >&2; exit 1; }

check-platform:
	@case "$$OS/$$ARCH" in \
		linux/amd64|linux/arm64|darwin/amd64|darwin/arm64) ;; \
		*) echo "Unsupported release platform: $$OS/$$ARCH" >&2; exit 1 ;; \
	esac

release-one: check-version check-platform | $(HAS_IMAGE_LOCAL)
	mkdir -p "$(DIR_STG)" "$(DIR_RELEASE)"
	$(CTR_RUN) -e GOOS="$$OS" -e GOARCH="$$ARCH" "$(CTR_IMAGE_LOCAL)" \
		go build -trimpath \
		-ldflags "-s -w -X github.com/cloudboss/unobin/cmd/unobin/root.Version=$$VERSION" \
		-o "$(DIR_STG)/unobin" ./cmd/unobin
	cp LICENSE README.md CHANGELOG.md "$(DIR_STG)/"
	tar -czf "$(DIR_RELEASE)/$(ARCHIVE)" -C "$(DIR_STG)" \
		unobin LICENSE README.md CHANGELOG.md
	cd "$(DIR_RELEASE)" && $(SHA256) "$(ARCHIVE)" > "$(ARCHIVE).sha256"

check-archive: check-version check-platform
	cd "$(DIR_RELEASE)" && $(SHA256) -c "$(ARCHIVE).sha256"

check-cli-version: check-archive
	mkdir -p "$(DIR_OUT)/release-check/$(VERSION)/$(OS)/$(ARCH)"
	tar -xzf "$(DIR_RELEASE)/$(ARCHIVE)" \
		-C "$(DIR_OUT)/release-check/$(VERSION)/$(OS)/$(ARCH)"
	test "$$("$(DIR_OUT)/release-check/$(VERSION)/$(OS)/$(ARCH)/unobin" version)" = "$$VERSION"

check-release-assets: check-version
	@for os in linux darwin; do \
		for arch in amd64 arm64; do \
			$(MAKE) check-archive OS=$$os ARCH=$$arch || exit $$?; \
		done; \
	done

release-linux-amd64:
	$(MAKE) release-one OS=linux ARCH=amd64

release-linux-arm64:
	$(MAKE) release-one OS=linux ARCH=arm64

release-darwin-amd64:
	$(MAKE) release-one OS=darwin ARCH=amd64

release-darwin-arm64:
	$(MAKE) release-one OS=darwin ARCH=arm64

release: release-linux-amd64 release-linux-arm64 release-darwin-amd64 release-darwin-arm64
