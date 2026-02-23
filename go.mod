module github.com/merkle-works/x402-gateway

go 1.24.3

replace github.com/bsv-blockchain/go-sdk => ../go-sdk

require github.com/bsv-blockchain/go-sdk v0.0.0-00010101000000-000000000000

require (
	github.com/pkg/errors v0.9.1 // indirect
	golang.org/x/crypto v0.39.0 // indirect
)
