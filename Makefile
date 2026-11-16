BINARY := matrix

.PHONY: build install run test fmt vet clean

build:
	go build -trimpath -ldflags "-s -w" -o $(BINARY) .

install:
	go install -trimpath .

run:
	go run . $(ARGS)

test:
	go test ./...

fmt:
	gofmt -w .

vet:
	go vet ./...

clean:
	rm -f $(BINARY)
