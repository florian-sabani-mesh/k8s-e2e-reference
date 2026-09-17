variable "namespace" {
  type = string
}

module "workload" {

  source    = "../microservice"
  name      = "wiremock"
  namespace = var.namespace
  image     = "wiremock/wiremock:3.13.1"
  args      = ["--port", "8080", "--max-request-journal-entries", "2000", "--disable-banner"]
  env = {
    JAVA_OPTS = "-Xms64m -Xmx256m"
  }

  ready_path  = "/__admin/health"
  health_path = "/__admin/health"
  memory      = "512Mi"
  nonroot     = false

}

