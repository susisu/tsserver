module github.com/susisu/tsserver

go 1.26

require (
	github.com/go-chi/chi/v5 v5.3.1
	github.com/microsoft/typescript-go/_tsserver/typeeval v0.0.0
	github.com/samber/slog-chi v1.19.1
)

require (
	github.com/go-json-experiment/json v0.0.0-20260623181947-01eb4420fa68 // indirect
	github.com/klauspost/cpuid/v2 v2.2.10 // indirect
	github.com/microsoft/typescript-go v0.0.0-20260708042240-2bd066d87f5b // indirect
	github.com/zeebo/xxh3 v1.1.0 // indirect
	go.opentelemetry.io/otel v1.29.0 // indirect
	go.opentelemetry.io/otel/trace v1.29.0 // indirect
	golang.org/x/sync v0.21.0 // indirect
	golang.org/x/sys v0.46.0 // indirect
	golang.org/x/text v0.38.0 // indirect
)

replace github.com/microsoft/typescript-go/_tsserver/typeeval => ./typeeval
