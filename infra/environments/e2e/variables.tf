variable "kubeconfig" {
  type = string
}

variable "image_tag" {
  type    = string
  default = "e2e"
}

variable "database_password" {

  type        = string
  sensitive   = true
  default     = "local-e2e-only"
  description = "Disposable local credential; never use this environment for production."

}

variable "token_encryption_key" {

  type      = string
  sensitive = true
  # Base64 of the 32 bytes "01234567890123456789012345678901" (AES-256). Deterministic
  # test key only; never a production secret.
  default     = "MDEyMzQ1Njc4OTAxMjM0NTY3ODkwMTIzNDU2Nzg5MDE="
  description = "Base64-encoded 32-byte key for encrypting OAuth tokens at rest."

}

variable "coinbase_client_secret" {

  type        = string
  sensitive   = true
  default     = "e2e-coinbase-client-secret"
  description = "Disposable local Coinbase OAuth client secret for the E2E environment."

}

