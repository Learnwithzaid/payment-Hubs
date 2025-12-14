variable "environment" {
  description = "Deployment environment (e.g. production, staging)"
  type        = string
  default     = "production"
}

variable "kms_key_arn" {
  description = "KMS key ARN for log encryption"
  type        = string
}

# AWS CloudWatch Log Group for Audit Logs
resource "aws_cloudwatch_log_group" "audit" {
  name              = "/pci-infra/${var.environment}/audit"
  retention_in_days = 400 # PCI DSS requires > 1 year
  kms_key_id        = var.kms_key_arn

  tags = {
    Environment = var.environment
    Compliance  = "PCI-DSS"
    Application = "auditd"
  }
}

# Example Output for use in application config
output "audit_log_group" {
  value = aws_cloudwatch_log_group.audit.name
}
