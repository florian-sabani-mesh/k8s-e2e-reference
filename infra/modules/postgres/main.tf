variable "namespace" {
  type = string
}

variable "password" {
  type      = string
  sensitive = true
}

resource "kubernetes_secret_v1" "database" {

  metadata {
    name      = "database"
    namespace = var.namespace
  }

  data = {
    password = var.password, url = "postgres://orders:${var.password}@postgres:5432/orders?sslmode=disable"
  }


}

resource "kubernetes_service_v1" "postgres" {

  metadata {
    name      = "postgres"
    namespace = var.namespace
  }

  spec {
    selector = {
      app = "postgres"
    }

    port {
      port        = 5432
      target_port = 5432
    }

  }


}

resource "kubernetes_stateful_set_v1" "postgres" {

  metadata {
    name      = "postgres"
    namespace = var.namespace
  }

  spec {

    service_name = "postgres"
    replicas     = 1
    selector {
      match_labels = {
        app = "postgres"
      }

    }

    template {

      metadata {
        labels = {
          app = "postgres"
        }

      }

      spec {

        automount_service_account_token = false
        container {

          name  = "postgres"
          image = "postgres:17.6-alpine3.22"
          env {
            name  = "POSTGRES_USER"
            value = "orders"
          }

          env {
            name  = "POSTGRES_DB"
            value = "orders"
          }

          env {
            name  = "PGDATA"
            value = "/var/lib/postgresql/data/pgdata"
          }

          env {
            name = "POSTGRES_PASSWORD"
            value_from {
              secret_key_ref {
                name = kubernetes_secret_v1.database.metadata[0].name
                key  = "password"
              }

            }

          }

          port {
            container_port = 5432
          }

          volume_mount {
            name       = "data"
            mount_path = "/var/lib/postgresql/data"
          }

          readiness_probe {
            exec {
              command = ["pg_isready", "-U", "orders", "-d", "orders"]
            }

            period_seconds = 2
          }

          liveness_probe {
            exec {
              command = ["pg_isready", "-U", "orders", "-d", "orders"]
            }

            initial_delay_seconds = 20
            period_seconds        = 10
          }

          resources {
            requests = {
              cpu = "100m", memory = "128Mi"
            }

            limits = {
              memory = "384Mi"
            }

          }


        }


      }


    }

    volume_claim_template {

      metadata {
        name = "data"
      }

      spec {
        access_modes = ["ReadWriteOnce"]
        resources {
          requests = {
            storage = "1Gi"
          }

        }

      }


    }


  }


}

output "secret_name" {
  value = kubernetes_secret_v1.database.metadata[0].name
}

