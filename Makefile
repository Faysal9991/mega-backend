SQLC_VERSION := v1.31.1
SQLC := go run github.com/sqlc-dev/sqlc/cmd/sqlc@$(SQLC_VERSION)

.PHONY: generate check-generated check

generate:
	$(SQLC) generate

check-generated:
	$(SQLC) diff

check:
	go build ./...
	go test -count=1 ./...
	go test -race -count=1 ./...
	go vet ./...
	git diff --check
