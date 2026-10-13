terraform {
  required_version = ">= 1.10"
  required_providers {
    cloudflare = {
      source  = "cloudflare/cloudflare"
      version = "~> 5.0"
    }
    aws    = { source = "hashicorp/aws", version = "~> 6.0" }
    random = { source = "hashicorp/random", version = "~> 3.6" }
  }
  backend "s3" {
    bucket       = "ayopa-tfstate-subdomainserver"
    key          = "prod/terraform.tfstate"
    region       = "eu-central-1"
    encrypt      = true
    use_lockfile = true
  }
}

provider "aws" {
  region = var.region
  default_tags { tags = { Project = var.project, ManagedBy = "terraform" } }
}
# Reads CLOUDFLARE_API_TOKEN from the environment. Never put it in a file.
provider "cloudflare" {}

data "aws_caller_identity" "current" {}
data "aws_availability_zones" "available" { state = "available" }

