output "cluster_id" {
  value = alicloud_cs_managed_kubernetes.this.id
}

output "kubeconfig_path" {
  value = local_file.kubeconfig.filename
}
