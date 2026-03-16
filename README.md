# Terraform Provider for Grepr

This Terraform provider enables you to manage Grepr pipelines (async streaming jobs) using Infrastructure as Code.

## Requirements

- [Terraform](https://www.terraform.io/downloads.html) >= 1.0
- [Go](https://golang.org/doc/install) >= 1.21 (for building from source)

## Quick Start

```bash
git clone https://github.com/grepr/grepr-terraform.git
cd grepr-terraform
make setup
```

This builds the provider and generates a `.terraformrc.local` file that tells Terraform to use the local binary.

Set your credentials:

```bash
export GREPR_HOST=https://yourorg.app.grepr.ai/
export GREPR_CLIENT_ID=your-client-id
export GREPR_CLIENT_SECRET=your-client-secret
```

Then run Terraform (no `terraform init` needed with `dev_overrides`):

```bash
cd examples/resources/grepr_pipeline
TF_CLI_CONFIG_FILE=/path/to/grepr-terraform/.terraformrc.local terraform plan
```

Or export it for the session:

```bash
export TF_CLI_CONFIG_FILE=/path/to/grepr-terraform/.terraformrc.local
terraform plan
terraform apply
```

### Alternative: Install to Terraform plugins directory

```bash
make install
```

This installs the provider to `~/.terraform.d/plugins/` so you can use `terraform init` as usual.

## Authentication

The provider uses OAuth 2.0 client credentials for authentication. You'll need a client ID and client secret from your Grepr organization.

### Configuration

```hcl
provider "grepr" {
  host          = "https://myorg.app.grepr.ai/"
  client_id     = var.grepr_client_id
  client_secret = var.grepr_client_secret

  # Optional: defaults to grepr-prod.us.auth0.com
  # auth0_domain = "grepr-prod.us.auth0.com"
}
```

### Environment Variables

You can also configure the provider using environment variables:

- `GREPR_HOST` - The Grepr API host URL
- `GREPR_CLIENT_ID` - OAuth client ID
- `GREPR_CLIENT_SECRET` - OAuth client secret
- `GREPR_AUTH0_DOMAIN` - Auth0 domain (optional)

## Resources

### grepr_pipeline

Manages a Grepr pipeline (async streaming job).

#### Example Usage

```hcl
resource "grepr_pipeline" "example" {
  name = "my_pipeline"

  job_graph_json = jsonencode({
    vertices = [
      {
        type          = "datadog-log-agent-source"
        name          = "source"
        integrationId = "0jn5rdc93r10t"
      },
      {
        type             = "grok-parser"
        name             = "parser"
        grokParsingRules = ["%{TIMESTAMP_ISO8601:ts} %{GREEDYDATA:msg}"]
      },
      {
        type      = "logs-iceberg-table-sink"
        name      = "sink"
        datasetId = "my-dataset-id"
      }
    ]
    edges = ["source -> parser", "parser -> sink"]
  })

  desired_state = "RUNNING"

  tags = {
    environment = "production"
    team        = "platform"
  }

  team_ids = ["team-id-1"]
}

output "pipeline_id" {
  value = grepr_pipeline.example.id
}

output "pipeline_state" {
  value = grepr_pipeline.example.state
}
```

#### Argument Reference

| Argument           | Type        | Required | Description                                                |
|--------------------|-------------|----------|------------------------------------------------------------|
| `name`             | string      | Yes      | The name of the pipeline. Must match `[a-z0-9_]{1,128}`.   |
| `job_graph_json`   | string      | Yes      | The job graph as a JSON string. Use `jsonencode()`.        |
| `desired_state`    | string      | No       | Desired state: `RUNNING` or `STOPPED`. Default: `RUNNING`. |
| `team_ids`         | set(string) | No       | Team IDs associated with this pipeline.                    |
| `tags`             | map(string) | No       | Custom tags for the pipeline.                              |
| `wait_for_state`   | bool        | No       | Wait for desired state after operations. Default: `true`.  |
| `state_timeout`    | number      | No       | Timeout in seconds for state transitions. Default: `600`.  |
| `rollback_enabled` | bool        | No       | Enable automatic rollback on failures. Default: `false`.   |

#### Attributes Reference

| Attribute          | Type   | Description                                                         |
|--------------------|--------|---------------------------------------------------------------------|
| `id`               | string | The unique identifier of the pipeline (TSID format).                |
| `version`          | number | The current version of the pipeline.                                |
| `state`            | string | The actual current state of the pipeline.                           |
| `organization_id`  | string | The organization ID that owns this pipeline.                        |
| `created_at`       | string | Timestamp when the pipeline was created.                            |
| `updated_at`       | string | Timestamp when the pipeline was last updated.                       |
| `pipeline_health`  | string | Health status: `HEALTHY`, `STABILIZING`, `UNHEALTHY`, or `UNKNOWN`. |
| `pipeline_message` | string | Human-readable status message.                                      |

#### Behavior

**Adopt Existing Pipelines**: If a pipeline with the specified name already exists, the provider will adopt it into Terraform management rather than failing. Any differences between the Terraform configuration and the existing pipeline will be applied as an update.

**Version Conflict Handling**: The provider uses optimistic locking. If a pipeline is modified by another process between read and update, the operation will fail with a conflict error. Run `terraform refresh` and retry.

**Import**: You can import existing pipelines by ID or name:

```bash
terraform import grepr_pipeline.example 0ABC12DEF4G
terraform import grepr_pipeline.example my_pipeline_name
```

## Development

### Building

```bash
make build
```

### Testing

```bash
make test        # Unit tests
make testacc     # Acceptance tests (requires GREPR_* env vars)
```

### Installing Locally

```bash
make install
```

This installs the provider to `~/.terraform.d/plugins/` for local testing.

## License

MIT - See [LICENSE](LICENSE) for details.
