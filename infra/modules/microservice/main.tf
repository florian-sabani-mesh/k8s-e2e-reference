variable "name" {
  type = string
}

variable "namespace" {
  type = string
}

variable "image" {
  type = string
}

variable "env" {
  type = map(string)
  default = {

  }

}

variable "database_secret" {
  type    = string
  default = ""
}

variable "secret_env" {
  description = "Extra environment variables sourced from Kubernetes Secrets: name => {secret, key}."
  type = map(object({
    secret = string
    key    = string
  }))
  default = {}
}

variable "args" {
  type    = list(string)
  default = []
}

variable "ready_path" {
  type    = string
  default = "/readyz"
}

variable "health_path" {
  type    = string
  default = "/healthz"
}

variable "memory" {
  type    = string
  default = "128Mi"
}

variable "nonroot" {
  type    = bool
  default = true
}

resource "kubernetes_config_map_v1" "config" {

  metadata {
    name      = var.name
    namespace = var.namespace
  }

  data = var.env

}

resource "kubernetes_deployment_v1" "this" {

  metadata {
    name      = var.name
    namespace = var.namespace
    labels = {
      app = var.name
    }

  }

  spec {

    replicas = 1
    selector {
      match_labels = {
        app = var.name
      }

    }

    template {

      metadata {
        labels = {
          app = var.name
        }

        annotations = {
          "config-checksum" = sha256(jsonencode(var.env))
        }

      }

      spec {

        automount_service_account_token  = false
        termination_grace_period_seconds = 20
        container {

          name              = var.name
          image             = var.image
          image_pull_policy = "IfNotPresent"
          args              = var.args
          port {
            container_port = 8080
          }

          env_from {
            config_map_ref {
              name = kubernetes_config_map_v1.config.metadata[0].name
            }

          }

          dynamic "env" {

            for_each = var.database_secret == "" ? [] : [var.database_secret]
            content {
              name = "DATABASE_URL"
              value_from {
                secret_key_ref {
                  name = env.value
                  key  = "url"
                }

              }

            }


          }

          dynamic "env" {

            for_each = var.secret_env
            content {
              name = env.key
              value_from {
                secret_key_ref {
                  name = env.value.secret
                  key  = env.value.key
                }

              }

            }


          }

          security_context {

            run_as_non_root            = var.nonroot
            allow_privilege_escalation = false
            read_only_root_filesystem  = var.nonroot
            capabilities {
              drop = ["ALL"]
            }


          }

          resources {
            requests = {
              cpu = "50m", memory = var.nonroot ? "32Mi" : "128Mi"
            }

            limits = {
              memory = var.memory
            }

          }

          startup_probe {
            http_get {
              path = var.health_path
              port = 8080
            }

            period_seconds    = 2
            failure_threshold = 90
          }

          readiness_probe {
            http_get {
              path = var.ready_path
              port = 8080
            }

            period_seconds  = 2
            timeout_seconds = 2
          }

          liveness_probe {
            http_get {
              path = var.health_path
              port = 8080
            }

            period_seconds  = 10
            timeout_seconds = 2
          }


        }


      }


    }


  }

  timeouts {
    create = "5m"
    update = "5m"
  }


}

resource "kubernetes_service_v1" "this" {

  metadata {
    name      = var.name
    namespace = var.namespace
  }

  spec {
    selector = {
      app = var.name
    }

    port {
      port        = 8080
      target_port = 8080
    }

  }


}

