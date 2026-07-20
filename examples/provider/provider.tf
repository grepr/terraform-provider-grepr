terraform {
  required_providers {
    grepr = {
      source  = "grepr/grepr"
      version = "~> 1.0"
    }
  }
}

# Credentials can also be supplied via the GREPR_HOST, GREPR_CLIENT_ID, and
# GREPR_CLIENT_SECRET environment variables instead of the provider block.
provider "grepr" {
  host          = "https://myorg.app.grepr.ai"
  client_id     = var.grepr_client_id
  client_secret = var.grepr_client_secret
}
