# typeeval/ is a separate Go module, so `go test ./...` from the root does not cover it.
.PHONY: test
test:
	go test ./...
	cd typeeval && go test ./...

.PHONY: fmt
fmt:
	gofmt -l -w . typeeval
