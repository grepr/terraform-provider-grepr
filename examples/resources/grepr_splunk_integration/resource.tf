variable "splunk_hec_token" {
  type      = string
  sensitive = true
}

resource "grepr_splunk_integration" "main" {
  name        = "production_splunk"
  splunk_host = "splunk.example.com"
  auth_token  = var.splunk_hec_token
}
