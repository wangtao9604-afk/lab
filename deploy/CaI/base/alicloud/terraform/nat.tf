# NAT 出网：EIP + NAT Gateway + SNAT
resource "alicloud_eip" "nat" {
  address_name         = "${var.business_name}-nat-eip"
  internet_charge_type = "PayByTraffic"
}

resource "alicloud_nat_gateway" "this" {
  vpc_id               = alicloud_vpc.this.id
  nat_gateway_name     = "${var.business_name}-nat"
  payment_type         = "PayAsYouGo"
  vswitch_id           = alicloud_vswitch.vsw_public.id
  nat_type             = "Enhanced"
  internet_charge_type = "PayByLcu"
}

resource "alicloud_eip_association" "nat_eip" {
  allocation_id = alicloud_eip.nat.id
  instance_id   = alicloud_nat_gateway.this.id
}

resource "alicloud_snat_entry" "snat_first" {
  snat_table_id     = alicloud_nat_gateway.this.snat_table_ids
  source_vswitch_id = alicloud_vswitch.vsw_first_private.id
  snat_ip           = alicloud_eip.nat.ip_address
}

resource "alicloud_snat_entry" "snat_second" {
  count             = var.use_multiaz ? 1 : 0
  snat_table_id     = alicloud_nat_gateway.this.snat_table_ids
  source_vswitch_id = alicloud_vswitch.vsw_second_private[0].id
  snat_ip           = alicloud_eip.nat.ip_address
}

resource "alicloud_snat_entry" "snat_first_pod" {
  snat_table_id = alicloud_nat_gateway.this.snat_table_ids
  source_cidr   = alicloud_vswitch.vsw_first_pod.cidr_block
  snat_ip       = alicloud_eip.nat.ip_address
}

resource "alicloud_snat_entry" "snat_second_pod" {
  count         = var.use_multiaz ? 1 : 0
  snat_table_id = alicloud_nat_gateway.this.snat_table_ids
  source_cidr   = alicloud_vswitch.vsw_second_pod[0].cidr_block
  snat_ip       = alicloud_eip.nat.ip_address
}
