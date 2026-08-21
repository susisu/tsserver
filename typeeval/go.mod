// This module's path is deliberately declared under github.com/microsoft/typescript-go/
// so that Go's internal-package rule permits importing typescript-go's internal API.
// It is never published; the root module references it via a replace directive.
module github.com/microsoft/typescript-go/_tsserver/typeeval

go 1.26

require github.com/microsoft/typescript-go v0.0.0-20260708042240-2bd066d87f5b // typescript/v7.0.2

require (
	github.com/go-json-experiment/json v0.0.0-20260623181947-01eb4420fa68 // indirect
	github.com/klauspost/cpuid/v2 v2.2.10 // indirect
	github.com/zeebo/xxh3 v1.1.0 // indirect
	golang.org/x/sync v0.21.0 // indirect
	golang.org/x/sys v0.46.0 // indirect
	golang.org/x/text v0.38.0 // indirect
)
