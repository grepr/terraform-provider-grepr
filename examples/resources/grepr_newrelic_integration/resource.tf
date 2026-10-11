variable "newrelic_license_key" {
  type      = string
  sensitive = true
}

resource "grepr_newrelic_integration" "main" {
  name        = "production_newrelic"
  site        = "newrelic.com"
  license_key = var.newrelic_license_key
}
