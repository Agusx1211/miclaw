.PHONY: all build test vet lint cross

build:
	go build ./cmd/miclaw

cross:
	./scripts/cross_compile_linux.sh

test:
	go test -race -count=1 ./...

vet:
	go vet ./...

lint:
	staticcheck ./...

all:
	$(MAKE) vet
	@if ! command -v staticcheck >/dev/null 2>&1; then \
		echo "staticcheck not installed, skipping lint"; \
	else \
		$(MAKE) lint; \
	fi
	$(MAKE) test
	$(MAKE) build
