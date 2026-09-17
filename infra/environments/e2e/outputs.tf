output "namespace" {
  value = local.namespace
}

output "gateway_service" {
  value = "gateway.${local.namespace}.svc.cluster.local:8080"
}

output "wiremock_service" {
  value = "wiremock.${local.namespace}.svc.cluster.local:8080"
}

