data "alicloud_cs_cluster_credential" "cred" {
  cluster_id                 = alicloud_cs_managed_kubernetes.this.id
  temporary_duration_minutes = 120
  depends_on                 = [alicloud_cs_kubernetes_node_pool.core]
}
