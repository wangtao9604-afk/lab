resource "alicloud_db_instance" "this" {
  engine           = "MySQL"
  engine_version   = var.rds_engine_version
  instance_type    = var.rds_instance_type
  instance_storage = var.rds_storage_gb
  vswitch_id       = join(",", var.vswitch_ids)
  zone_id          = var.zone_id
  zone_id_slave_a  = var.enable_multi_az ? var.zone_id_slave_a : null
  zone_id_slave_b  = var.enable_multi_az ? var.zone_id_slave_b : null

  instance_charge_type = var.pay_type == "Prepaid" ? "Prepaid" : "Postpaid"
  period               = var.pay_type == "Prepaid" ? var.period : null
  auto_renew           = var.pay_type == "Prepaid" ? var.auto_renew : null

  security_ips = var.security_cidrs
}

resource "alicloud_db_account" "admin" {
  db_instance_id   = alicloud_db_instance.this.id
  account_name     = var.rds_account
  account_password = var.rds_password
  account_type     = "Normal"
}

resource "alicloud_db_database" "business" {
  instance_id   = alicloud_db_instance.this.id
  name          = var.db_name
  character_set = "utf8mb4"
}

resource "alicloud_db_account_privilege" "admin_rw" {
  instance_id  = alicloud_db_instance.this.id
  account_name = alicloud_db_account.admin.account_name
  privilege    = "ReadWrite"
  db_names     = [alicloud_db_database.business.name]
}
