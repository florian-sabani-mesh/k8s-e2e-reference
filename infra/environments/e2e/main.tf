resource "kubernetes_namespace_v1" "e2e" {
  metadata {
    name = "e2e"
  }

}

locals {

  namespace = kubernetes_namespace_v1.e2e.metadata[0].name
  common = {
    APP_MODE = "e2e", NATS_URL = "nats://nats.e2e.svc.cluster.local:4222"
  }


}

module "postgres" {
  source    = "../../modules/postgres"
  namespace = local.namespace
  password  = var.database_password
}

module "nats" {
  source    = "../../modules/nats"
  namespace = local.namespace
}

module "wiremock" {
  source    = "../../modules/wiremock"
  namespace = local.namespace
}

# Terraform waits for Postgres readiness, then for the one-shot migration job.
# Application deployments depend on successful migrations, so readiness cannot race DDL.
resource "kubernetes_job_v1" "migrate" {

  metadata {
    name      = format("migrate-%s", substr(sha256(join("", [for file in sort(fileset("${path.module}/../../../migrations", "*.sql")) : filesha256("${path.module}/../../../migrations/${file}")])), 0, 12))
    namespace = local.namespace
  }

  spec {

    backoff_limit           = 3
    active_deadline_seconds = 120
    template {

      metadata {
        labels = {
          app = "migrate"
        }

      }

      spec {

        restart_policy                  = "Never"
        automount_service_account_token = false
        container {

          name              = "migrate"
          image             = "reference/orders:${var.image_tag}"
          image_pull_policy = "IfNotPresent"
          command           = ["/app/migrate"]
          env {
            name = "DATABASE_URL"
            value_from {
              secret_key_ref {
                name = module.postgres.secret_name
                key  = "url"
              }

            }

          }

          security_context {
            run_as_non_root            = true
            allow_privilege_escalation = false
          }


        }


      }


    }


  }

  wait_for_completion = true
  timeouts {
    create = "3m"
  }

  depends_on = [module.postgres]

}

module "orders" {

  source          = "../../modules/microservice"
  name            = "orders"
  namespace       = local.namespace
  image           = "reference/orders:${var.image_tag}"
  database_secret = module.postgres.secret_name
  env = merge(local.common, {
    SERVICE_NAME = "orders"
    }
  )
  depends_on = [kubernetes_job_v1.migrate, module.nats]

}

module "pricing" {

  source    = "../../modules/microservice"
  name      = "pricing"
  namespace = local.namespace
  image     = "reference/pricing:${var.image_tag}"
  env = merge(local.common, {

    SERVICE_NAME          = "pricing"
    COINBASE_BASE_URL     = "http://wiremock.e2e.svc.cluster.local:8080/coinbase"
    KRAKEN_BASE_URL       = "http://wiremock.e2e.svc.cluster.local:8080/kraken"
    KUCOIN_BASE_URL       = "http://wiremock.e2e.svc.cluster.local:8080/kucoin"
    EXCHANGE_TIMEOUT      = "400ms"
    EXCHANGE_BACKOFF      = "100ms"
    EXCHANGE_MAX_ATTEMPTS = "3"

    }
  )
  depends_on = [module.nats, module.wiremock, kubernetes_job_v1.migrate]

}

module "settlement" {

  source          = "../../modules/microservice"
  name            = "settlement"
  namespace       = local.namespace
  image           = "reference/settlement:${var.image_tag}"
  database_secret = module.postgres.secret_name
  env = merge(local.common, {
    SERVICE_NAME = "settlement"
    }
  )
  depends_on = [module.nats, kubernetes_job_v1.migrate]

}

# Coinbase OAuth client secret and the token-encryption key are delivered as a
# Kubernetes Secret rather than through the ConfigMap that carries plain env.
resource "kubernetes_secret_v1" "exchange" {

  metadata {
    name      = "exchange"
    namespace = local.namespace
  }

  data = {
    "token-encryption-key"   = var.token_encryption_key
    "coinbase-client-secret" = var.coinbase_client_secret
  }

}

module "exchange" {

  source          = "../../modules/microservice"
  name            = "exchange"
  namespace       = local.namespace
  image           = "reference/exchange:${var.image_tag}"
  database_secret = module.postgres.secret_name
  env = merge(local.common, {

    SERVICE_NAME = "exchange"
    # USD valuation is delegated to the pricing service over real Kubernetes HTTP.
    PRICING_URL = "http://pricing.e2e.svc.cluster.local:8080"
    # Every Coinbase endpoint is pinned to WireMock; APP_MODE=e2e refuses public hosts.
    COINBASE_AUTHORIZE_URL = "http://wiremock.e2e.svc.cluster.local:8080/coinbase-oauth/oauth2/auth"
    COINBASE_TOKEN_URL     = "http://wiremock.e2e.svc.cluster.local:8080/coinbase-oauth/oauth2/token"
    COINBASE_API_BASE_URL  = "http://wiremock.e2e.svc.cluster.local:8080/coinbase-api"
    COINBASE_CLIENT_ID     = "e2e-coinbase-client-id"
    # Coinbase's registered redirect_uri points at our callback, reached via the gateway.
    COINBASE_CALLBACK_URL  = "http://gateway.e2e.svc.cluster.local:8080/exchanges/coinbase/callback"
    REDIRECT_URL_ALLOWLIST = "http://localhost:3000,http://127.0.0.1:3000"
    COINBASE_TIMEOUT       = "2s"

    }
  )
  secret_env = {
    TOKEN_ENCRYPTION_KEY = {
      secret = kubernetes_secret_v1.exchange.metadata[0].name
      key    = "token-encryption-key"
    }
    COINBASE_CLIENT_SECRET = {
      secret = kubernetes_secret_v1.exchange.metadata[0].name
      key    = "coinbase-client-secret"
    }
  }
  depends_on = [module.pricing, module.wiremock, kubernetes_job_v1.migrate]

}

module "gateway" {

  source    = "../../modules/microservice"
  name      = "gateway"
  namespace = local.namespace
  image     = "reference/gateway:${var.image_tag}"
  env = {
    SERVICE_NAME = "gateway"
    ORDERS_URL   = "http://orders.e2e.svc.cluster.local:8080"
    EXCHANGE_URL = "http://exchange.e2e.svc.cluster.local:8080"
  }

  depends_on = [module.orders, module.exchange]

}

