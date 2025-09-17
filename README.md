# argocd-game-tools

一个针对 Argo CD gRPC API 的轻量 CLI。当前提供 `app down` 命令，将应用内可缩容工作负载（Deployment/StatefulSet/GameDeployment/GameStatefulSet）按 SyncWave 逆序依次缩容到 0，并在每个工作负载完成后继续下一个。

## 安装

从 Releases 下载对应平台二进制，或本地构建：

```bash
go build -o argocd-game-tools .
```

## 使用

```bash
./argocd-game-tools app down <app-name> \
  --server <host:port> \
  [--tls-no-verify] \
  [--auth-token $ARGOCD_AUTH_TOKEN | --username <user> --password <pass>] \
  [--project <project>] \
  [--no-grace] [--grace-period 0] \
  [--grpc-web] [--grpc-web-root-path /api]
```

- `--project`: 指定应用所属 project，用于资源过滤与权限校验。
- `--no-grace`/`--grace-period`: 强制删除挂住的 Pod（可指定宽限期）。
- `--tls-no-verify`: 跳过 TLS 校验（自签证书时常用）。
- `--grpc-web`: 通过 grpc-web 代理模式连接（在部分 Ingress/反向代理下需要）。

示例：

```bash
./argocd-game-tools app down demo-app \
  --server 127.0.0.1:49909 \
  --tls-no-verify \
  --auth-token "$ARGOCD_AUTH_TOKEN" \
  --project default
```
