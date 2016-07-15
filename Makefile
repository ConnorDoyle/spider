.PHONY: \
	all \
	deps \
	glide \
	lint \
	test

MKFILE_DIR := $(abspath $(dir $(abspath $(lastword $(MAKEFILE_LIST)))))

PKG_PREFIXES := pkg
SOURCES := $(foreach p,$(PKG_PREFIXES),./$(p)/...)

TEST_BUILD_TAGS := small

all: deps format lint test

format:
	@ echo "Formatting source code"
	git ls-files "**.go" | xargs -n1 gofmt -e -s -w

lint:
	@ if ! which fgt > /dev/null; then \
			echo "fgt not found, attempting to install" >&2; \
			if ! go get github.com/GeertJohan/fgt; then \
				exit 1; \
			fi \
		fi
	@ if ! which golint > /dev/null; then \
			echo "Golint not found, attempting to install" >&2; \
			if ! go get github.com/golang/lint/golint; then \
				exit 1; \
			fi \
		fi
	@ echo "Linting source code"
	echo $(SOURCES) | xargs -n1 fgt golint

test:
	@ echo "Running unit tests"
	go test -v $(SOURCES) -tags $(TEST_BUILD_TAGS)

deps: glide
	@ echo "Restoring source dependencies"
	glide install

glide:
	@ if ! which glide > /dev/null; then \
			echo "Glide not found, attempting to install" >&2; \
			if ! go get github.com/Masterminds/glide; then \
				exit 1; \
			fi \
		fi
