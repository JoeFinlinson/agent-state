.PHONY: build test clean install

build:
	go build -o bin/as ./cmd/as

test:
	go test ./... -cover

clean:
	rm -f bin/as

install: build
	cp bin/as /usr/local/bin/as
