resource "alicloud_cs_managed_kubernetes" "this" {
  name                         = var.cluster_name
  version                      = var.k8s_version
  cluster_spec                 = "ack.pro.small" // ack pro
  vswitch_ids                  = var.worker_vswitch_ids
  pod_vswitch_ids              = var.pod_vswitch_ids
  new_nat_gateway              = var.create_new_nat_gateway
  service_cidr                 = "172.21.0.0/20"
  is_enterprise_security_group = true
  proxy_mode                   = "ipvs"

  addons {
    name = "terway-eniip"
  }

  addons {
    name = "csi-plugin"
  }

  addons {
    name = "csi-provisioner"
  }
}

resource "alicloud_cs_kubernetes_node_pool" "system" {
  cluster_id           = alicloud_cs_managed_kubernetes.this.id
  node_pool_name       = "nodepool-system"
  vswitch_ids          = var.worker_vswitch_ids
  instance_types       = [var.system_instance_type]
  system_disk_category = "cloud_essd"
  system_disk_size     = 120
  desired_size         = 2

  instance_charge_type = var.node_pay_type
  period               = var.node_pay_type == "PrePaid" ? var.node_period : null
  period_unit          = var.node_pay_type == "PrePaid" ? var.node_period_unit : null
  auto_renew           = var.node_pay_type == "PrePaid" ? var.node_auto_renew : null

  # scaling_config {
  #   enable   = true
  #   min_size = 2
  #   max_size = 2
  #   type     = "cpu"
  # }

  labels {
    key   = "pool"
    value = "system"
  }
}

resource "alicloud_cs_kubernetes_node_pool" "core" {
  cluster_id           = alicloud_cs_managed_kubernetes.this.id
  node_pool_name       = "nodepool-core"
  vswitch_ids          = var.worker_vswitch_ids
  instance_types       = [var.core_instance_type]
  system_disk_category = "cloud_essd"
  system_disk_size     = 120
  desired_size         = 1

  instance_charge_type = var.node_pay_type
  period               = var.node_pay_type == "PrePaid" ? var.node_period : null
  period_unit          = var.node_pay_type == "PrePaid" ? var.node_period_unit : null
  auto_renew           = var.node_pay_type == "PrePaid" ? var.node_auto_renew : null

  # scaling_config {
  #   enable   = true
  #   min_size = 2
  #   max_size = 2
  #   type     = "cpu"
  # }

  labels {
    key   = "pool"
    value = "core"
  }

  taints {
    key    = "pool"
    value  = "core"
    effect = "NoSchedule"
  }
}

resource "alicloud_cs_kubernetes_node_pool" "consumers" {
  cluster_id           = alicloud_cs_managed_kubernetes.this.id
  node_pool_name       = "nodepool-consumers"
  vswitch_ids          = var.worker_vswitch_ids
  instance_types       = [var.consumers_instance_type]
  system_disk_category = "cloud_essd" #"cloud_efficiency"
  system_disk_size     = 120
  desired_size         = 1

  instance_charge_type = var.node_pay_type
  period               = var.node_pay_type == "PrePaid" ? var.node_period : null
  period_unit          = var.node_pay_type == "PrePaid" ? var.node_period_unit : null
  auto_renew           = var.node_pay_type == "PrePaid" ? var.node_auto_renew : null

  # scaling_config {
  #   enable   = true
  #   min_size = 2
  #   max_size = 2
  #   type     = "cpu"
  # }

  labels {
    key   = "pool"
    value = "consumers"
  }

  taints {
    key    = "pool"
    value  = "consumers"
    effect = "NoSchedule"
  }
}

resource "local_file" "kubeconfig" {
  content  = data.alicloud_cs_cluster_credential.cred.kube_config
  filename = "${path.module}/kubeconfig_${alicloud_cs_managed_kubernetes.this.id}"
}
