all: mocks

.PHONY: mocks
mocks:
	@find -name 'mock_*.go' -delete
	@mockery --all --inpackage --case underscore

