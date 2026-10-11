variable "otlp_auth_header" {
  type      = string
  sensitive = true
}

resource "grepr_otlp_integration" "main" {
  name     = "production_otlp"
  endpoint = "https://otlp.example.com"
  headers  = [{ name = "X-Team", value = "platform" }]

  secret_headers = {
    Authorization = var.otlp_auth_header
  }
}

output "otlp_ingestion_token" {
  value     = grepr_otlp_integration.main.ingestion_token
  sensitive = true
}
