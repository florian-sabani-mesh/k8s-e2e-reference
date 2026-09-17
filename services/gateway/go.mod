module example.com/terraform-k8s-e2e-reference/services/gateway

go 1.27.0

require example.com/terraform-k8s-e2e-reference/pkg/runtime v0.0.0

require (
	example.com/terraform-k8s-e2e-reference/pkg/events v0.0.0 // indirect
	github.com/klauspost/compress v1.18.0 // indirect
	github.com/nats-io/nats.go v1.47.0 // indirect
	github.com/nats-io/nkeys v0.4.11 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	golang.org/x/crypto v0.37.0 // indirect
	golang.org/x/sys v0.32.0 // indirect
)

replace example.com/terraform-k8s-e2e-reference/pkg/runtime => ../../pkg/runtime

replace example.com/terraform-k8s-e2e-reference/pkg/events => ../../pkg/events
