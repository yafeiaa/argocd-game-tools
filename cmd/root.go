package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	serverAddr  string
	insecure    bool
	tlsNoVerify bool
	username    string
	password    string
	authToken   string
	grpcWeb     bool
	grpcWebRoot string
)

// rootCmd is the base command
var rootCmd = &cobra.Command{
	Use:           "agt",
	Short:         "Argo CD gRPC 封装 CLI",
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute runs the root command
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&serverAddr, "server", os.Getenv("ARGOCD_SERVER"), "Argo CD API server 地址 (host:port)")
	rootCmd.PersistentFlags().BoolVar(&insecure, "insecure", false, "允许明文/不安全连接（开发环境）")
	rootCmd.PersistentFlags().BoolVar(&tlsNoVerify, "tls-no-verify", false, "跳过 TLS 证书校验")
	rootCmd.PersistentFlags().StringVar(&username, "username", os.Getenv("ARGOCD_USERNAME"), "用户名（与 --password 搭配）")
	rootCmd.PersistentFlags().StringVar(&password, "password", os.Getenv("ARGOCD_PASSWORD"), "密码（与 --username 搭配）")
	rootCmd.PersistentFlags().StringVar(&authToken, "auth-token", os.Getenv("ARGOCD_AUTH_TOKEN"), "Bearer Token（优先于用户名密码）")
	rootCmd.PersistentFlags().BoolVar(&grpcWeb, "grpc-web", false, "启用 grpc-web 代理模式（避免直连 gRPC 阻塞）")
	rootCmd.PersistentFlags().StringVar(&grpcWebRoot, "grpc-web-root-path", "", "grpc-web 根路径（经由反向代理时使用，如 /api")
}
