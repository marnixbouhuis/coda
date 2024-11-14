.PHONY: test
test:
	go test -v ./...

.PHONY: lint
lint:
	# You can install golangci-lint using: make install-tools
	golangci-lint run ./... --fix

.PHONY:
dependencies:
	go mod tidy

.PHONY: install-tools
install-tools:
	(cd tools && \
		go mod download && \
		cat tools.go | grep _ | awk -F'"' '{print $$2}' | xargs -tI % go install % \
	)
