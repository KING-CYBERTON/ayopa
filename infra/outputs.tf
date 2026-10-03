output "public_ip" { value = aws_eip.app.public_ip }
output "instance_id" { value = aws_instance.app.id }
output "ecr_app" { value = aws_ecr_repository.app.repository_url }
output "ecr_caddy" { value = aws_ecr_repository.caddy.repository_url }
output "artifacts_bucket" { value = aws_s3_bucket.artifacts.bucket }
output "gha_role_arn" { value = aws_iam_role.gha_deploy.arn }