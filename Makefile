.PHONY: build clean

install:
	GOBIN=$(HOME)/.local/bin go install ./cmd/nib/

clean:
	rm -f nib-bin
