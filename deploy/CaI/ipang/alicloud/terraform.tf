terraform {
  required_version = "1.13.4"
  required_providers {
    alicloud = {
      source  = "aliyun/alicloud"
      version = "1.260.1"
    }
    kubernetes = {
      source  = "hashicorp/kubernetes"
      version = "2.38.0"
    }
    helm = {
      source  = "hashicorp/helm"
      version = "3.0.2"
    }
    local = {
      source  = "hashicorp/local"
      version = "2.5.3"
    }
  }
}
