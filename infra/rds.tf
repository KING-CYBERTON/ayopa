resource "random_password" "db" {
  length  = 32
  special = false # avoids escaping problems in connection strings
}

resource "aws_db_subnet_group" "main" { subnet_ids = aws_subnet.private[*].id }

resource "aws_db_instance" "main" {
  identifier              = "${var.project}-db"
  engine                  = "postgres"
  engine_version          = "16"
  instance_class          = "db.t4g.micro"
  allocated_storage       = 20
  storage_encrypted       = true
  db_name                 = "ayopa_subdomainserver"
  username                = "ayopa_admin"
  password                = random_password.db.result
  db_subnet_group_name    = aws_db_subnet_group.main.name
  vpc_security_group_ids  = [aws_security_group.db.id]
  publicly_accessible     = false
  backup_retention_period = 7
  deletion_protection     = !var.allow_destroy
  skip_final_snapshot     = var.allow_destroy
  final_snapshot_identifier = var.allow_destroy ? null : "${var.project}-final"
}

resource "aws_ssm_parameter" "database_url" {
  name  = "/${var.project}/database_url"
  type  = "SecureString"
  value = "postgres://ayopa_admin:${random_password.db.result}@${aws_db_instance.main.address}:5432/ayopa?sslmode=require"
}