locals {
  mall_records = merge([
    for domain, zone_id in var.mall_zones : {
      "${domain}-apex" = {
        zone_id = zone_id
        name    = domain
      }
      "${domain}-wildcard" = {
        zone_id = zone_id
        name    = "*.${domain}"
      }
    }
  ]...)
}

# DNS only (not proxied), so Caddy terminates TLS itself.
resource "cloudflare_dns_record" "mall" {
  for_each = local.mall_records

  zone_id = each.value.zone_id
  name    = each.value.name
  type    = "A"
  content = aws_eip.app.public_ip
  ttl     = 300
  proxied = false
}