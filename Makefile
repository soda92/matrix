BINARY := matrix

.PHONY: build install run test fmt vet demo clean

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) .

install:
	go install -trimpath -ldflags "$(LDFLAGS)" .

run:
	go run . $(ARGS)

test:
	go test ./...

fmt:
	gofmt -w .

vet:
	go vet ./...

demo: build
	python3 hack/render-demo.py --bin ./$(BINARY) --out docs/demo.gif

clean:
	rm -f $(BINARY)
