.PHONY: build test clean install

build:
	go build -o bin/st ./cmd/as

test:
	go test ./... -cover

clean:
	rm -f bin/st

install: build
	@# Resolve symlinks — write directly to the final target
	@target=$$(readlink /usr/local/bin/st 2>/dev/null || echo /usr/local/bin/st); \
	rm -f "$$target"; \
	cp bin/st "$$target"; \
	xattr -cr "$$target" 2>/dev/null || true
