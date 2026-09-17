module example.com/terraform-k8s-e2e-reference/services/exchange

go 1.27.0

require (
	example.com/terraform-k8s-e2e-reference/pkg/runtime v0.0.0
	github.com/google/uuid v1.6.0
	github.com/jackc/pgx/v5 v5.7.6
	github.com/shopspring/decimal v1.4.0
)

require (
	example.com/terraform-k8s-e2e-reference/pkg/events v0.0.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/klauspost/compress v1.18.0 // indirect
	github.com/nats-io/nats.go v1.47.0 // indirect
	github.com/nats-io/nkeys v0.4.11 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	golang.org/x/crypto v0.37.0 // indirect
	golang.org/x/sync v0.13.0 // indirect
	golang.org/x/sys v0.32.0 // indirect
	golang.org/x/text v0.24.0 // indirect
)

replace example.com/terraform-k8s-e2e-reference/pkg/runtime => ../../pkg/runtime

replace example.com/terraform-k8s-e2e-reference/pkg/events => ../../pkg/events
