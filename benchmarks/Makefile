.PHONY: build-mock build-harness round1 round2 all clean

build-mock:
	go build -o bin/mock-server ./cmd/mock-server/...

build-harness:
	go build -o bin/harness ./cmd/harness/...

round1: build-mock build-harness
	./bin/harness run --round 1

round2: build-mock build-harness
	./bin/harness run --round 2

all: build-mock build-harness round1 round2

clean:
	rm -rf bin/ results/*.json results/*.csv results/*.md
