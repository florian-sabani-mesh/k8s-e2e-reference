terraform {

  required_version = ">= 1.14.0, < 2.0.0"
  required_providers {
    kubernetes = {
      source = "hashicorp/kubernetes", version = "2.38.0"
    }

  }

  backend "local" {

  }


}

provider "kubernetes" {

  config_path    = var.kubeconfig
  config_context = "kind-terraform-k8s-e2e-reference"

}

