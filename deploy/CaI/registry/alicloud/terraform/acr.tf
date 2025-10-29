resource "alicloud_cr_namespace" "acr_ns" {
  name               = "ns-${var.business_name}-278845"
  auto_create        = false
  default_visibility = "PRIVATE"
}

resource "alicloud_cr_repo" "acr" {
  name      = var.business_name
  namespace = alicloud_cr_namespace.acr_ns.name
  summary   = "micro service images of ${var.business_name}"
  repo_type = "PRIVATE"
}
