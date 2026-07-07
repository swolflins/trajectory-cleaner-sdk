.PHONY: build test clean eval visualize

build:
	go build ./...

test:
	go test ./... -v -count=1

clean:
	rm -rf bin

eval:
	go run cmd/eval/main.go

visualize:
	go run cmd/visualize/main.go
