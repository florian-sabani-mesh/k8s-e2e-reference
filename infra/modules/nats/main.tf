variable "namespace" {
  type = string
}

resource "kubernetes_config_map_v1" "nats" {

  metadata {
    name      = "nats"
    namespace = var.namespace
  }

  data = {
    "nats.conf" = "port: 4222\nhttp_port: 8222\njetstream { store_dir: /data/jetstream }\n"
  }


}

resource "kubernetes_service_v1" "nats" {

  metadata {
    name      = "nats"
    namespace = var.namespace
  }

  spec {

    selector = {
      app = "nats"
    }

    port {
      name        = "client"
      port        = 4222
      target_port = 4222
    }

    port {
      name        = "monitor"
      port        = 8222
      target_port = 8222
    }


  }


}

resource "kubernetes_stateful_set_v1" "nats" {

  metadata {
    name      = "nats"
    namespace = var.namespace
  }

  spec {

    service_name = "nats"
    replicas     = 1
    selector {
      match_labels = {
        app = "nats"
      }

    }

    template {

      metadata {
        labels = {
          app = "nats"
        }

      }

      spec {

        automount_service_account_token = false
        security_context {
          run_as_user  = 1000
          run_as_group = 1000
          fs_group     = 1000
        }

        container {

          name  = "nats"
          image = "nats:2.11.9-alpine"
          args  = ["-c", "/etc/nats/nats.conf"]
          port {
            container_port = 4222
          }

          port {
            container_port = 8222
          }

          volume_mount {
            name       = "config"
            mount_path = "/etc/nats"
            read_only  = true
          }

          volume_mount {
            name       = "data"
            mount_path = "/data"
          }

          readiness_probe {
            http_get {
              path = "/healthz?js-enabled-only=true"
              port = 8222
            }

            period_seconds = 2
          }

          liveness_probe {
            http_get {
              path = "/healthz"
              port = 8222
            }

            period_seconds = 10
          }

          resources {
            requests = {
              cpu = "50m", memory = "32Mi"
            }

            limits = {
              memory = "128Mi"
            }

          }


        }

        volume {
          name = "config"
          config_map {
            name = kubernetes_config_map_v1.nats.metadata[0].name
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

