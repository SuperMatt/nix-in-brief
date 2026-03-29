.PHONY: install clean

install:
	GOBIN=$(HOME)/.local/bin go install ./cmd/nib/

clean:
	go clean -i ./cmd/nib/
