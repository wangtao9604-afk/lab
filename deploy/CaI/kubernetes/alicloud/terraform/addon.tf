resource "alicloud_cs_kubernetes_addon" "addons" {
  cluster_id = alicloud_cs_managed_kubernetes.this.id
  name       = "alb-ingress-controller"

  config = jsonencode({
    albIngress = {
      CreateDefaultALBConfig = true
      AddressType            = "Internet"
      ZoneMappings = [
        {
          ZoneId    = var.first_az
          VSwitchId = var.public_vswitch_id_in_first_az
        },
        {
          ZoneId    = var.second_az
          VSwitchId = var.public_vswitch_id_in_second_az
        }
      ]
    }
  })

  version = var.alb_ingress_controller_version
}
