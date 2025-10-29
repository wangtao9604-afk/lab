output "repo_endpoint" {
  value       = local.public_host != null ? "${local.public_host}/${local.repo_path}" : local.repo_path
  description = "Full endpoint for the container registry repository (public host if available, otherwise namespace/repo)."
}
