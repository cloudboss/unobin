# Copyright © 2026 Joseph Wright <joseph@cloudboss.co>
#
# Permission is hereby granted, free of charge, to any person obtaining a copy
# of this software and associated documentation files (the "Software"), to deal
# in the Software without restriction, including without limitation the rights
# to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
# copies of the Software, and to permit persons to whom the Software is
# furnished to do so, subject to the following conditions:
#
# The above copyright notice and this permission notice shall be included in
# all copies or substantial portions of the Software.
#
# THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
# IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
# FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
# AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
# LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
# OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
# THE SOFTWARE.

VERSION ?=
DIR_OUT ?= _output
DIR_ROOT = $(realpath $(CURDIR))
DIR_OUT_ABS = $(abspath $(DIR_OUT))
DIR_STG = $(DIR_OUT)/staging/unobin/$(VERSION)/$(OS)/$(ARCH)
DIR_RELEASE ?= $(DIR_OUT)/release

CTR_IMAGE_GO = ghcr.io/cloudboss/docker.io/library/golang:1.26.2-trixie
CTR_IMAGE_GOLANGCI = ghcr.io/cloudboss/golangci/golangci-lint:v2.11.4-alpine
OS ?= $(shell uname -s | tr '[:upper:]' '[:lower:]')
ARCH ?= $(shell uname -m | sed -e 's/x86_64/amd64/' -e 's/aarch64/arm64/')
ARCHIVE = unobin-$(VERSION)-$(OS)-$(ARCH).tar.gz
USER_ID = $(shell id -u)
GROUP_ID = $(shell id -g)

SHA256 ?= $(shell command -v sha256sum >/dev/null 2>&1 && echo sha256sum || echo 'shasum -a 256')
BUILD_IMAGE_HASH = $(shell { \
	printf '%s\n' '$(CTR_IMAGE_GO)' '$(USER_ID)' '$(GROUP_ID)'; \
	cat Containerfile.build; \
	} | $(SHA256) | cut -c 1-40)
CTR_IMAGE_LOCAL = unobin-build:$(BUILD_IMAGE_HASH)
HAS_IMAGE_LOCAL = $(DIR_OUT)/.image-local-$(BUILD_IMAGE_HASH)
HAS_COMMAND_DOCKER = $(DIR_OUT)/.command-docker

CTR_RUN = docker run --rm \
	-v $(DIR_ROOT):$(DIR_ROOT):z \
	-w $(DIR_ROOT) \
	-e GOPATH=$(DIR_OUT_ABS)/go \
	-e GOCACHE=$(DIR_OUT_ABS)/gocache \
	-e XDG_CACHE_HOME=$(DIR_OUT_ABS)/cache \
	-e npm_config_cache=$(DIR_OUT_ABS)/cache/npm \
	-e CGO_ENABLED=0

export VERSION OS ARCH

.DEFAULT_GOAL := test

$(DIR_OUT):
	@mkdir -p $(DIR_OUT)

$(DIR_OUT)/%/:
	@mkdir -p $(DIR_OUT)/$*

$(DIR_OUT)/.command-%: | $(DIR_OUT)
	@[ -f $(DIR_OUT)/.command-$* ] || { \
		which $* >/dev/null 2>&1 && \
		touch $(DIR_OUT)/.command-$* || \
		(echo "command $(*) is required"; exit 1); \
	}

.PHONY: check-archive check-cli-version check-platform check-release \
	check-release-assets check-version lint \
	release release-darwin-amd64 release-darwin-arm64 \
	release-linux-amd64 release-linux-arm64 release-one test

$(HAS_IMAGE_LOCAL): Containerfile.build | $(DIR_OUT) $(HAS_COMMAND_DOCKER)
	@docker build \
		--build-arg FROM=$(CTR_IMAGE_GO) \
		--build-arg USER_ID=$(USER_ID) \
		--build-arg GROUP_ID=$(GROUP_ID) \
		-f Containerfile.build -t $(CTR_IMAGE_LOCAL) .
	@touch ${@}

test: | $(HAS_IMAGE_LOCAL)
	@$(CTR_RUN) $(CTR_IMAGE_LOCAL) sh -c \
		'go vet ./... && go test -timeout 30m ./... && \
		go -C benchmarks vet ./... && go -C benchmarks test ./...'

lint: | $(DIR_OUT) $(HAS_COMMAND_DOCKER)
	@$(CTR_RUN) -u $(USER_ID):$(GROUP_ID) \
		-e GOLANGCI_LINT_CACHE=$(DIR_OUT_ABS)/cache/golangci-lint \
		$(CTR_IMAGE_GOLANGCI) sh -c \
		'golangci-lint run --timeout 5m ./... && \
		cd benchmarks && golangci-lint run --timeout 5m ./...'

check-version:
	@number='(0|[1-9][0-9]*)'; \
	prerelease='(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)'; \
	printf '%s\n' "$${VERSION}" | LC_ALL=C grep -Eq \
		"^v$${number}\.$${number}\.$${number}(-$${prerelease}(\.$${prerelease})*)?$$" || \
		{ echo 'VERSION must be vMAJOR.MINOR.PATCH with an optional prerelease suffix' >&2; exit 1; }

check-release: check-version
	@case $${VERSION} in \
		*-*) echo 'Release versions must be vMAJOR.MINOR.PATCH without a suffix' >&2; exit 1 ;; \
	esac
	@awk -v version=$${VERSION} \
		'$${1} == "##" && $${2} == "[" substr(version, 2) "]" && $${3} == "-" && \
		$${4} ~ /^[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]$$/ { found = 1 } \
		END { exit !found }' CHANGELOG.md || \
		{ echo "CHANGELOG.md needs a dated entry for $${VERSION}" >&2; exit 1; }

check-platform:
	@case $${OS}/$${ARCH} in \
		linux/amd64|linux/arm64|darwin/amd64|darwin/arm64) ;; \
		*) echo "Unsupported release platform: $${OS}/$${ARCH}" >&2; exit 1 ;; \
	esac

release-one: check-version check-platform | $(DIR_STG)/ $(DIR_RELEASE)/ $(HAS_IMAGE_LOCAL)
	@$(CTR_RUN) -e GOOS=$${OS} -e GOARCH=$${ARCH} $(CTR_IMAGE_LOCAL) \
		go build -trimpath \
		-ldflags "-s -w -X github.com/cloudboss/unobin/cmd/unobin/root.Version=$${VERSION}" \
		-o $(DIR_STG)/unobin ./cmd/unobin
	@cp LICENSE CHANGELOG.md $(DIR_STG)/
	@tar -czf $(DIR_RELEASE)/$(ARCHIVE) -C $(DIR_STG) unobin LICENSE CHANGELOG.md
	@cd $(DIR_RELEASE) && $(SHA256) $(ARCHIVE) > $(ARCHIVE).sha256

check-archive: release-one
	@cd $(DIR_RELEASE) && $(SHA256) -c $(ARCHIVE).sha256

check-cli-version: check-archive | $(DIR_OUT)/release-check/$(VERSION)/$(OS)/$(ARCH)/
	@tar -xzf $(DIR_RELEASE)/$(ARCHIVE) \
		-C $(DIR_OUT)/release-check/$(VERSION)/$(OS)/$(ARCH)
	@version=$$($(DIR_OUT)/release-check/$(VERSION)/$(OS)/$(ARCH)/unobin version) && \
		[ "$${version}" = "$${VERSION}" ]

check-release-assets: check-version
	@for os in linux darwin; do \
		for arch in amd64 arm64; do \
			$(MAKE) check-archive OS=$${os} ARCH=$${arch} || exit $${?}; \
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
