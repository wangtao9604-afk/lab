locals {
  repo_path        = "${alicloud_cr_repo.acr.namespace}/${alicloud_cr_repo.acr.name}"
  repo_domain_list = try(alicloud_cr_repo.acr.domain_list, [])
  public_host      = length(local.repo_domain_list) > 0 ? try(local.repo_domain_list[0].public, null) : null
}
